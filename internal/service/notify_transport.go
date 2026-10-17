package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/notify"
	"doctolibre/internal/notify/sms"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// Paramètres des fournisseurs par cabinet.
const (
	// TestSendsPerHour borne les messages de test par heure et par cabinet.
	TestSendsPerHour = 5
	testSendTimeout  = 20 * time.Second
	maxSettingLen    = 2000
	maxLongSettLen   = 4000

	// Actions du journal d'audit.
	ActionNotifyTransportUpdated = "notify_transport_updated"
	ActionNotifyTransportTest    = "notify_transport_test"
)

// Erreurs des fournisseurs par cabinet.
var (
	// ErrTestRateLimited : trop de messages de test sur la dernière heure.
	ErrTestRateLimited = errors.New("trop de messages de test")
	// ErrChannelDisabled : le cabinet a désactivé ce canal.
	ErrChannelDisabled = notify.ErrChannelDisabled
	// ErrBookingClosed : plus aucun canal de vérification actif, la réservation en ligne est fermée.
	ErrBookingClosed = errors.New("réservation en ligne fermée : aucun canal de vérification actif")
)

// TestError est l'échec d'un message de test : seul LogID est montré à l'utilisateur,
// le détail est dans les logs du serveur.
type TestError struct{ LogID string }

func (e TestError) Error() string { return "échec de l'envoi de test (" + e.LogID + ")" }

// Channels indique quels canaux de messages sont actifs (mode ≠ désactivé) pour un cabinet.
type Channels struct{ Email, SMS bool }

// Any indique si au moins un canal est actif.
func (c Channels) Any() bool { return c.Email || c.SMS }

// Has indique si le canal ("email" | "sms") est actif.
func (c Channels) Has(channel string) bool {
	switch channel {
	case channelEmail:
		return c.Email
	case channelSMS:
		return c.SMS
	}
	return false
}

// channelsOf lit l'état des canaux du cabinet (pas de ligne = fournisseur du serveur = actif).
func channelsOf(ctx context.Context, q *sqlcgen.Queries, practiceID uuid.UUID) (Channels, error) {
	rows, err := q.ListNotifyTransports(ctx, practiceID)
	if err != nil {
		return Channels{}, fmt.Errorf("lecture des fournisseurs: %w", err)
	}
	c := Channels{Email: true, SMS: true}
	for _, r := range rows {
		if r.Mode != notify.ModeDisabled {
			continue
		}
		switch r.Kind {
		case notify.KindEmail:
			c.Email = false
		case notify.KindSMS:
			c.SMS = false
		}
	}
	return c, nil
}

// normalizeChannel ramène un réglage de canal par événement aux canaux actifs :
// « both » devient l'autre canal, un canal désactivé devient « none ».
func normalizeChannel(c string, on Channels) string {
	switch c {
	case ChannelEmail:
		if !on.Email {
			return ChannelNone
		}
	case ChannelSMS:
		if !on.SMS {
			return ChannelNone
		}
	case ChannelBoth:
		switch {
		case on.Email && on.SMS:
		case on.Email:
			return ChannelEmail
		case on.SMS:
			return ChannelSMS
		default:
			return ChannelNone
		}
	}
	return c
}

// NotifyTransport gère les fournisseurs d'email et de SMS d'un cabinet.
type NotifyTransport struct {
	Pool       *pgxpool.Pool
	Clock      domain.Clock
	Transports *notify.Transports
	Logger     *slog.Logger

	mu    sync.Mutex
	tests map[uuid.UUID][]time.Time
}

// NewNotifyTransport construit le service.
func NewNotifyTransport(pool *pgxpool.Pool, clock domain.Clock, t *notify.Transports, logger *slog.Logger) *NotifyTransport {
	return &NotifyTransport{Pool: pool, Clock: clock, Transports: t, Logger: logger}
}

func (s *NotifyTransport) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// requireTransportRole : les fournisseurs engagent le cabinet, comme ses réglages de
// notification : administrateur uniquement (T056).
func requireTransportRole(a Actor) error {
	return requireAdmin(a)
}

// TransportView est l'état d'un canal affiché dans l'administration. Aucun secret :
// SecretSet dit seulement lesquels sont enregistrés.
type TransportView struct {
	Kind, Mode, Driver string
	Settings           map[string]string
	SecretSet          map[string]bool
	// Unreadable : les identifiants enregistrés ne se déchiffrent plus (clé secrète changée).
	Unreadable bool
	// ServerDriver : fournisseur du serveur (smtp, log, ovh…), sans ses paramètres.
	ServerDriver string
	// ServerLogInProd : le fournisseur du serveur est « log » en production (rien n'est envoyé).
	ServerLogInProd bool
}

// TransportsView regroupe les deux canaux.
type TransportsView struct{ Email, SMS TransportView }

func (s *NotifyTransport) viewOf(kind string, tr notify.Transport) TransportView {
	v := TransportView{Kind: kind, Mode: tr.Mode, Driver: tr.Driver, Settings: tr.Settings, SecretSet: map[string]bool{}, Unreadable: tr.Unreadable}
	if v.Settings == nil {
		v.Settings = map[string]string{}
	}
	for k, val := range tr.Secrets {
		v.SecretSet[k] = val != ""
	}
	env := s.Transports.Env
	if kind == notify.KindEmail {
		v.ServerDriver = env.Email.Driver
	} else {
		v.ServerDriver = env.SMS.Driver
		if v.ServerDriver == "" {
			v.ServerDriver = config.SMSDriverLog
		}
	}
	v.ServerLogInProd = env.IsProd() && v.ServerDriver == config.EmailDriverLog
	return v
}

// View retourne l'état des deux canaux du cabinet.
func (s *NotifyTransport) View(ctx context.Context, a Actor) (TransportsView, error) {
	if err := requireTransportRole(a); err != nil {
		return TransportsView{}, err
	}
	q := sqlcgen.New(s.Pool)
	var out TransportsView
	for _, k := range []struct {
		kind string
		dst  *TransportView
	}{{notify.KindEmail, &out.Email}, {notify.KindSMS, &out.SMS}} {
		tr, err := s.Transports.Load(ctx, q, a.PracticeID, k.kind)
		if err != nil {
			return TransportsView{}, err
		}
		*k.dst = s.viewOf(k.kind, tr)
	}
	return out, nil
}

// TransportInput est la saisie d'un canal. Secrets : nouvelles valeurs (vide =
// valeur conservée) ; Clear : secrets à effacer.
type TransportInput struct {
	Kind, Mode, Driver string
	Settings           map[string]string
	Secrets            map[string]string
	Clear              map[string]bool
}

// TransportResult décrit ce que l'enregistrement a changé en plus du fournisseur.
type TransportResult struct {
	// SettingsAdjusted : des réglages de notification par événement utilisaient le canal désactivé et ont été modifiés.
	SettingsAdjusted bool
}

var configLabels = []string{
	"DOCTOLIBRE_SMTP_USER et DOCTOLIBRE_SMTP_PASSWORD", "L'identifiant et le mot de passe",
	"DOCTOLIBRE_SMTP_HOST", "Serveur SMTP", "DOCTOLIBRE_SMTP_FROM", "Expéditeur",
	"DOCTOLIBRE_SMTP_TLS", "Chiffrement", "DOCTOLIBRE_SMS_HTTP_URL", "URL", "DOCTOLIBRE_SMS_HTTP_BODY", "Corps de la requête",
	"DOCTOLIBRE_SMS_ANDROID_USER", "Identifiant", "DOCTOLIBRE_SMS_ANDROID_PASSWORD", "Mot de passe",
	"DOCTOLIBRE_SMS_OVH_SERVICE", "Service OVH", "DOCTOLIBRE_SMS_OVH_APP_KEY", "Clé d'application",
	"DOCTOLIBRE_SMS_OVH_APP_SECRET", "Secret d'application", "DOCTOLIBRE_SMS_OVH_CONSUMER_KEY", "Clé consommateur",
}

// friendlyConfigErrors traduit les erreurs de config (qui citent des variables
// d'environnement) en messages pour l'administration.
func friendlyConfigErrors(err error, driver string) domain.ValidationErrors {
	repl := strings.NewReplacer(append([]string{"DOCTOLIBRE_SMS_DRIVER=" + driver, "ce fournisseur"}, configLabels...)...)
	var out domain.ValidationErrors
	for _, line := range strings.Split(err.Error(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, domain.ErrValidation{Field: "transport", Msg: repl.Replace(line) + "."})
		}
	}
	return out
}

func clean(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) > limit {
		return s[:limit]
	}
	return s
}

// Update enregistre le fournisseur d'un canal. Les identifiants sont chiffrés ;
// désactiver un canal ramène, dans la même transaction, les réglages de
// notification qui l'utilisaient.
func (s *NotifyTransport) Update(ctx context.Context, a Actor, in TransportInput) (TransportResult, error) {
	if err := requireTransportRole(a); err != nil {
		return TransportResult{}, err
	}
	if in.Kind != notify.KindEmail && in.Kind != notify.KindSMS {
		return TransportResult{}, domain.ErrValidation{Field: "kind", Msg: "Canal inconnu."}
	}
	switch in.Mode {
	case notify.ModeServer, notify.ModeCustom, notify.ModeDisabled:
	default:
		return TransportResult{}, domain.ErrValidation{Field: "mode", Msg: "Choisissez le fournisseur du serveur, le vôtre, ou désactivé."}
	}
	var res TransportResult
	err := store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		if _, err := q.GetPractice(ctx, a.PracticeID); errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		} else if err != nil {
			return fmt.Errorf("lecture cabinet: %w", err)
		}
		old, err := s.Transports.Load(ctx, q, a.PracticeID, in.Kind)
		if err != nil {
			return err
		}
		next := notify.Transport{Mode: in.Mode}
		secretsChanged := false
		if in.Mode == notify.ModeCustom {
			fields, ok := notify.FieldsOf(in.Kind, in.Driver)
			if !ok {
				return domain.ErrValidation{Field: "driver", Msg: "Choisissez un fournisseur."}
			}
			next.Driver = in.Driver
			next.Settings, next.Secrets = map[string]string{}, map[string]string{}
			for _, f := range fields {
				switch {
				case !f.Secret:
					limit := maxSettingLen
					if f.Key == "body" {
						limit = maxLongSettLen
					}
					if v := clean(in.Settings[f.Key], limit); v != "" {
						next.Settings[f.Key] = v
					}
				case in.Clear[f.Key]:
					secretsChanged = secretsChanged || old.Secrets[f.Key] != ""
				case in.Secrets[f.Key] != "":
					next.Secrets[f.Key] = clean(in.Secrets[f.Key], maxLongSettLen)
					secretsChanged = true
				case old.Driver == in.Driver && old.Secrets[f.Key] != "":
					next.Secrets[f.Key] = old.Secrets[f.Key]
				}
			}
			if err := s.Transports.Check(in.Kind, next); err != nil {
				if errors.Is(err, notify.ErrForbiddenHost) {
					return domain.ErrValidation{Field: "transport", Msg: "Cette adresse vise un réseau interne et n'est pas autorisée."}
				}
				var cfgErrs domain.ValidationErrors
				if strings.Contains(err.Error(), "DOCTOLIBRE_") {
					cfgErrs = friendlyConfigErrors(err, in.Driver)
				} else {
					cfgErrs = domain.ValidationErrors{{Field: "transport", Msg: "Configuration invalide : " + err.Error() + "."}}
				}
				return cfgErrs
			}
		}
		settings, secrets, err := s.Transports.Encode(a.PracticeID, in.Kind, next)
		if err != nil {
			return err
		}
		if err := q.UpsertNotifyTransport(ctx, sqlcgen.UpsertNotifyTransportParams{
			PracticeID: a.PracticeID, Kind: in.Kind, Mode: next.Mode,
			Driver: pgtype.Text{String: next.Driver, Valid: next.Driver != ""}, Settings: settings, Secrets: secrets,
		}); err != nil {
			return fmt.Errorf("enregistrement du fournisseur: %w", err)
		}
		if next.Mode == notify.ModeDisabled {
			if res.SettingsAdjusted, err = adjustChannels(ctx, q, a.PracticeID); err != nil {
				return err
			}
		}
		meta := map[string]string{"kind": in.Kind, "mode": next.Mode, "previous_mode": old.Mode}
		if next.Driver != "" {
			meta["driver"] = next.Driver
		}
		if old.Driver != next.Driver {
			meta["driver_changed"] = "true"
		}
		if secretsChanged {
			meta["secrets"] = "modified"
		}
		return s.audit(ctx, q, a, ActionNotifyTransportUpdated, meta)
	})
	return res, err
}

// adjustChannels ramène les réglages par événement aux canaux actifs ; true si quelque chose a changé.
func adjustChannels(ctx context.Context, q *sqlcgen.Queries, practiceID uuid.UUID) (bool, error) {
	pc, err := q.GetPractice(ctx, practiceID)
	if err != nil {
		return false, fmt.Errorf("lecture cabinet: %w", err)
	}
	on, err := channelsOf(ctx, q, practiceID)
	if err != nil {
		return false, err
	}
	p := sqlcgen.SetPracticeNotificationChannelsParams{
		ID:                        practiceID,
		NotifyConfirmationChannel: normalizeChannel(pc.NotifyConfirmationChannel, on),
		NotifyReminderChannel:     normalizeChannel(pc.NotifyReminderChannel, on),
		NotifyChangeChannel:       normalizeChannel(pc.NotifyChangeChannel, on),
	}
	if p.NotifyConfirmationChannel == pc.NotifyConfirmationChannel && p.NotifyReminderChannel == pc.NotifyReminderChannel &&
		p.NotifyChangeChannel == pc.NotifyChangeChannel {
		return false, nil
	}
	if err := q.SetPracticeNotificationChannels(ctx, p); err != nil {
		return false, fmt.Errorf("réglages de notification: %w", err)
	}
	return true, nil
}

func (s *NotifyTransport) audit(ctx context.Context, q *sqlcgen.Queries, a Actor, action string, meta map[string]string) error {
	b, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("meta audit: %w", err)
	}
	if err := q.CreateAuditLog(ctx, sqlcgen.CreateAuditLogParams{
		UserID: pgtype.UUID{Bytes: a.UserID, Valid: true}, PracticeID: pgtype.UUID{Bytes: a.PracticeID, Valid: true}, Actor: "user",
		Action: action, Entity: "practice", EntityID: pgtype.UUID{Bytes: a.PracticeID, Valid: true}, Meta: b,
	}); err != nil {
		return fmt.Errorf("journal d'audit: %w", err)
	}
	return nil
}

// allowTest applique la limite de TestSendsPerHour messages de test par heure et par cabinet.
func (s *NotifyTransport) allowTest(practiceID uuid.UUID) bool {
	now := s.Clock.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tests == nil {
		s.tests = map[uuid.UUID][]time.Time{}
	}
	recent := s.tests[practiceID][:0:0]
	for _, t := range s.tests[practiceID] {
		if now.Sub(t) < time.Hour {
			recent = append(recent, t)
		}
	}
	if len(recent) >= TestSendsPerHour {
		s.tests[practiceID] = recent
		return false
	}
	s.tests[practiceID] = append(recent, now)
	return true
}

// SendTest envoie directement (hors outbox, hors transaction) un court message
// avec la configuration enregistrée : à l'email de l'utilisateur connecté pour
// l'email, au numéro to pour le SMS. Retourne ErrTestRateLimited, une
// domain.ErrValidation, ErrChannelDisabled, ou une TestError (détail dans les logs).
func (s *NotifyTransport) SendTest(ctx context.Context, a Actor, kind, to string) error {
	if err := requireTransportRole(a); err != nil {
		return err
	}
	q := sqlcgen.New(s.Pool)
	var phone string
	if kind == notify.KindSMS {
		n, err := domain.NormalizePhone(to)
		if err != nil {
			return domain.ErrValidation{Field: "phone", Msg: "Le numéro de téléphone n'est pas valide (exemple : 06 12 34 56 78)."}
		}
		phone = n
	} else if kind != notify.KindEmail {
		return domain.ErrValidation{Field: "kind", Msg: "Canal inconnu."}
	}
	user, err := q.GetUser(ctx, a.UserID)
	if err != nil {
		return fmt.Errorf("lecture utilisateur: %w", err)
	}
	tr, err := s.Transports.Load(ctx, q, a.PracticeID, kind)
	if err != nil {
		return err
	}
	switch {
	case tr.Mode == notify.ModeDisabled:
		return ErrChannelDisabled
	case tr.Unreadable:
		return notify.ErrUnreadable
	}
	if !s.allowTest(a.PracticeID) {
		return ErrTestRateLimited
	}
	res, err := s.Transports.Resolve(ctx, q, a.PracticeID, kind)
	if err == nil {
		sctx, cancel := context.WithTimeout(ctx, testSendTimeout)
		defer cancel()
		if kind == notify.KindEmail {
			_, err = res.Email.Send(sctx, notify.Email{
				To: user.Email, Subject: "Test d'envoi Doctolibre",
				Text: "Ce message confirme que l'envoi d'emails fonctionne pour votre cabinet.\n",
			})
		} else {
			body := "Doctolibre : message de test, l'envoi de SMS fonctionne."
			if res.Transliterate {
				body = sms.Transliterate(body)
			}
			_, err = res.SMS.Send(sctx, sms.SMS{To: phone, Body: body, Sender: res.From})
		}
	}
	outcome := "ok"
	var out error
	if err != nil {
		outcome = "failed"
		out = s.testFailure(a, kind, tr, err)
	}
	if aerr := store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		return s.audit(ctx, q, a, ActionNotifyTransportTest, map[string]string{"kind": kind, "mode": tr.Mode, "result": outcome})
	}); aerr != nil {
		s.logger().Error("journal d'audit du test en échec", "practice_id", a.PracticeID, "kind", kind)
	}
	return out
}

// testFailure journalise l'échec (sans identifiant ni secret) et retourne ce qui peut être montré.
func (s *NotifyTransport) testFailure(a Actor, kind string, tr notify.Transport, err error) error {
	if errors.Is(err, notify.ErrForbiddenHost) {
		return domain.ErrValidation{Field: "transport", Msg: "Cette adresse vise un réseau interne et n'est pas autorisée."}
	}
	id := uuid.NewString()
	msg := err.Error()
	for _, v := range tr.Secrets {
		if v != "" {
			msg = strings.ReplaceAll(msg, v, "***")
		}
	}
	for _, k := range []string{"user", "basic_user"} {
		if v := tr.Settings[k]; v != "" {
			msg = strings.ReplaceAll(msg, v, "***")
		}
	}
	s.logger().Error("test d'envoi en échec", "log_id", id, "practice_id", a.PracticeID, "kind", kind, "mode", tr.Mode, "error", msg)
	return TestError{LogID: id}
}

// BookingClosedError : la réservation en ligne (et la liste d'attente) est fermée
// parce qu'aucun canal ne peut envoyer le code de vérification. Is(ErrBookingClosed).
type BookingClosedError struct{ Phone string }

func (BookingClosedError) Error() string { return ErrBookingClosed.Error() }

// Is permet errors.Is(err, ErrBookingClosed).
func (BookingClosedError) Is(target error) bool { return target == ErrBookingClosed }

// checkOTPChannel refuse l'envoi d'un code par un canal désactivé ou quand
// aucun canal n'est actif (le service ne se fie pas à l'interface).
func checkOTPChannel(ctx context.Context, q *sqlcgen.Queries, pc sqlcgen.Practice, channel string) error {
	ch, err := channelsOf(ctx, q, pc.ID)
	if err != nil {
		return err
	}
	if !ch.Any() {
		return BookingClosedError{Phone: pc.Phone}
	}
	if !ch.Has(channel) {
		return domain.ErrValidation{Field: "channel", Msg: "Ce mode de réception du code n'est pas disponible. Choisissez l'autre."}
	}
	return nil
}

// Channels retourne les canaux actifs du cabinet (réglages de notification, bandeau du tableau de bord).
func (s *Practice) Channels(ctx context.Context, a Actor) (Channels, error) {
	return channelsOf(ctx, sqlcgen.New(s.Pool), a.PracticeID)
}
