package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// Paramètres de la purge RGPD.
const (
	// DefaultPurgeEvery : la purge tourne une fois par jour.
	DefaultPurgeEvery = 24 * time.Hour
	scrubBatchSize    = 1000
	// PurgePreviewMax : nombre de patients listés dans la prévisualisation.
	PurgePreviewMax = 50
	// orphanAuditDays : conservation des lignes d'audit sans cabinet (compte inconnu).
	orphanAuditDays   = 365
	maxPrivacyContact = 200
)

// Actions d'audit RGPD.
const (
	ActionPatientView       = "patient_view"
	ActionPatientExported   = "patient.exported"
	ActionPatientAnonymized = "patient.anonymized"
	ActionPatientDeleted    = "patient.deleted"
	ActionPrivacyUpdated    = "privacy.settings_updated"
)

// Privacy porte les réglages de conservation, la purge périodique et la page
// d'information des patients.
type Privacy struct {
	Pool   *pgxpool.Pool
	Clock  domain.Clock
	Logger *slog.Logger
	// Every : période de Run (défaut DefaultPurgeEvery).
	Every time.Duration
}

func (s *Privacy) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// PrivacySettings sont les durées de conservation d'un cabinet.
type PrivacySettings struct {
	OutboxDays   int // contenu des messages envoyés, en jours
	PatientYears int // inactivité avant anonymisation, en années
	AuditDays    int // journal d'audit, en jours
	PurgeEnabled bool
	Contact      string // où exercer ses droits (email, adresse)
	Slug         string // du cabinet (lien vers la page publique)
}

func settingsOf(p sqlcgen.Practice) PrivacySettings {
	return PrivacySettings{
		OutboxDays: int(p.RetentionOutboxDays), PatientYears: int(p.RetentionPatientYears),
		AuditDays: int(p.RetentionAuditDays), PurgeEnabled: p.PatientPurgeEnabled, Contact: p.PrivacyContact, Slug: p.Slug,
	}
}

// Settings retourne les réglages de conservation du cabinet.
func (s *Privacy) Settings(ctx context.Context, a Actor) (PrivacySettings, error) {
	p, err := sqlcgen.New(s.Pool).GetPractice(ctx, a.PracticeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return PrivacySettings{}, domain.ErrNotFound
	}
	if err != nil {
		return PrivacySettings{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	return settingsOf(p), nil
}

// UpdateSettings valide et enregistre les réglages (journalisé).
func (s *Privacy) UpdateSettings(ctx context.Context, a Actor, in PrivacySettings) error {
	in.Contact = strings.TrimSpace(in.Contact)
	var errs domain.ValidationErrors
	if in.OutboxDays < 1 || in.OutboxDays > 3650 {
		errs = append(errs, domain.ErrValidation{Field: "retention_outbox_days", Msg: "La durée de conservation des messages doit être comprise entre 1 et 3650 jours."})
	}
	if in.PatientYears < 1 || in.PatientYears > 30 {
		errs = append(errs, domain.ErrValidation{Field: "retention_patient_years", Msg: "La durée avant anonymisation des patients doit être comprise entre 1 et 30 ans."})
	}
	if in.AuditDays < 30 || in.AuditDays > 3650 {
		errs = append(errs, domain.ErrValidation{Field: "retention_audit_days", Msg: "La durée de conservation du journal doit être comprise entre 30 et 3650 jours."})
	}
	if len([]rune(in.Contact)) > maxPrivacyContact {
		errs = append(errs, domain.ErrValidation{Field: "privacy_contact", Msg: fmt.Sprintf("Le contact est limité à %d caractères.", maxPrivacyContact)})
	}
	if len(errs) > 0 {
		return errs
	}
	// Bornes validées ci-dessus : les conversions ne débordent pas.
	outboxDays, patientYears, auditDays := int32(in.OutboxDays), int32(in.PatientYears), int32(in.AuditDays) //nolint:gosec // bornes validées
	return store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		if _, err := q.SetPracticePrivacy(ctx, sqlcgen.SetPracticePrivacyParams{
			ID: a.PracticeID, RetentionOutboxDays: outboxDays, RetentionPatientYears: patientYears,
			RetentionAuditDays: auditDays, PatientPurgeEnabled: in.PurgeEnabled, PrivacyContact: in.Contact,
		}); err != nil {
			return fmt.Errorf("enregistrement des réglages: %w", err)
		}
		return writeAudit(ctx, q, a.PracticeID, uuid.NullUUID{UUID: a.UserID, Valid: true}, ActionPrivacyUpdated, "practice", a.PracticeID, map[string]string{
			"outbox_days": fmt.Sprint(in.OutboxDays), "patient_years": fmt.Sprint(in.PatientYears),
			"audit_days": fmt.Sprint(in.AuditDays), "purge_enabled": fmt.Sprint(in.PurgeEnabled),
		})
	})
}

// PublicNotice retourne le cabinet dont on affiche les mentions d'information (par slug).
func (s *Privacy) PublicNotice(ctx context.Context, slug string) (sqlcgen.Practice, error) {
	p, err := sqlcgen.New(s.Pool).GetPracticeBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlcgen.Practice{}, domain.ErrNotFound
	}
	if err != nil {
		return sqlcgen.Practice{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	return p, nil
}

// writeAudit écrit une ligne du journal : uniquement des identifiants et des
// compteurs dans meta, jamais de nom, téléphone, email ou contenu de message.
// user invalide = action du système (purge automatique).
func writeAudit(ctx context.Context, q *sqlcgen.Queries, practiceID uuid.UUID, user uuid.NullUUID, action, entity string, entityID uuid.UUID, meta map[string]string) error {
	b, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("meta audit: %w", err)
	}
	actor := "system"
	if user.Valid {
		actor = "user"
	}
	if err := q.CreateAuditLog(ctx, sqlcgen.CreateAuditLogParams{
		UserID: pgtype.UUID{Bytes: user.UUID, Valid: user.Valid}, PracticeID: pgtype.UUID{Bytes: practiceID, Valid: true},
		Actor: actor, Action: action, Entity: entity, EntityID: pgtype.UUID{Bytes: entityID, Valid: true}, Meta: b,
	}); err != nil {
		return fmt.Errorf("journal d'audit: %w", err)
	}
	return nil
}

// PurgeCandidate est un patient que la purge automatique anonymiserait.
type PurgeCandidate struct {
	ID           uuid.UUID
	Name         string
	LastActivity time.Time
}

// PurgePreview est la prévisualisation de la purge des patients inactifs.
type PurgePreview struct {
	Cutoff     time.Time // inactifs depuis avant cette date
	Total      int64
	Candidates []PurgeCandidate // au plus PurgePreviewMax
}

// Preview liste les patients qui seraient anonymisés si la purge était lancée maintenant.
func (s *Privacy) Preview(ctx context.Context, a Actor) (PurgePreview, error) {
	set, err := s.Settings(ctx, a)
	if err != nil {
		return PurgePreview{}, err
	}
	cutoff := s.Clock.Now().AddDate(-set.PatientYears, 0, 0)
	q := sqlcgen.New(s.Pool)
	total, err := q.CountInactivePatients(ctx, sqlcgen.CountInactivePatientsParams{PracticeID: a.PracticeID, Before: cutoff})
	if err != nil {
		return PurgePreview{}, fmt.Errorf("comptage des patients inactifs: %w", err)
	}
	rows, err := q.ListInactivePatients(ctx, sqlcgen.ListInactivePatientsParams{
		PracticeID: a.PracticeID, AfterID: uuid.Nil, Before: cutoff, PageSize: PurgePreviewMax,
	})
	if err != nil {
		return PurgePreview{}, fmt.Errorf("liste des patients inactifs: %w", err)
	}
	out := PurgePreview{Cutoff: cutoff, Total: total}
	for _, r := range rows {
		out.Candidates = append(out.Candidates, PurgeCandidate{ID: r.ID, Name: r.LastName + " " + r.FirstName, LastActivity: r.LastActivity})
	}
	return out, nil
}

// PurgeResult compte ce qu'un tour de purge a traité.
type PurgeResult struct {
	MessagesScrubbed    int64
	SessionsDeleted     int64
	PasswordResets      int64
	AuditDeleted        int64
	PatientsAnonymized  int
	OTPChallengesPurged bool
}

// Run lance RunOnce tout de suite puis à chaque période, jusqu'à l'annulation de ctx.
func (s *Privacy) Run(ctx context.Context) error {
	every := s.Every
	if every <= 0 {
		every = DefaultPurgeEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		res, err := s.RunOnce(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			s.log().Error("purge RGPD en échec", "error", err)
		case err == nil:
			s.log().Info("purge RGPD terminée",
				"messages_scrubbed", res.MessagesScrubbed, "sessions", res.SessionsDeleted,
				"password_resets", res.PasswordResets, "audit_deleted", res.AuditDeleted, "patients_anonymized", res.PatientsAnonymized)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// RunOnce applique les durées de conservation de tous les cabinets. Idempotent.
// Une erreur sur un cabinet n'empêche pas de traiter les autres ; les erreurs
// sont regroupées.
func (s *Privacy) RunOnce(ctx context.Context) (PurgeResult, error) {
	var res PurgeResult
	if s.Pool == nil {
		return res, errors.New("purge: pas de base de données")
	}
	now := s.Clock.Now()
	q := sqlcgen.New(s.Pool)
	var errs []error

	n, err := q.DeleteExpiredSessions(ctx, now)
	res.SessionsDeleted = n
	errs = append(errs, wrapPurge("sessions expirées", err))
	n, err = q.DeleteExpiredPasswordResets(ctx, now)
	res.PasswordResets = n
	errs = append(errs, wrapPurge("jetons de réinitialisation expirés", err))
	err = q.DeleteOldOTPChallenges(ctx, now)
	res.OTPChallengesPurged = err == nil
	errs = append(errs, wrapPurge("codes de vérification expirés", err))
	n, err = q.DeleteOrphanAuditLogBefore(ctx, now.AddDate(0, 0, -orphanAuditDays))
	res.AuditDeleted += n
	errs = append(errs, wrapPurge("journal sans cabinet", err))

	after := uuid.Nil
	for {
		practices, err := q.ListPracticesPage(ctx, sqlcgen.ListPracticesPageParams{AfterID: after, PageSize: practicePageSize})
		if err != nil {
			errs = append(errs, fmt.Errorf("liste des cabinets: %w", err))
			break
		}
		for _, pc := range practices {
			if err := s.purgePractice(ctx, q, pc, now, &res); err != nil {
				errs = append(errs, fmt.Errorf("cabinet %s: %w", pc.ID, err))
			}
		}
		if len(practices) < practicePageSize {
			break
		}
		after = practices[len(practices)-1].ID
	}
	return res, errors.Join(errs...)
}

func wrapPurge(what string, err error) error {
	if err != nil {
		return fmt.Errorf("purge des %s: %w", what, err)
	}
	return nil
}

func (s *Privacy) purgePractice(ctx context.Context, q *sqlcgen.Queries, pc sqlcgen.Practice, now time.Time, res *PurgeResult) error {
	// Contenu des messages terminés : par lots.
	before := now.AddDate(0, 0, -int(pc.RetentionOutboxDays))
	for {
		n, err := q.ScrubOutboxBatch(ctx, sqlcgen.ScrubOutboxBatchParams{Now: now, PracticeID: pc.ID, Before: before, BatchSize: scrubBatchSize})
		if err != nil {
			return fmt.Errorf("effacement des messages: %w", err)
		}
		res.MessagesScrubbed += n
		if n < scrubBatchSize {
			break
		}
	}
	n, err := q.DeleteAuditLogBefore(ctx, sqlcgen.DeleteAuditLogBeforeParams{PracticeID: pgtype.UUID{Bytes: pc.ID, Valid: true}, Before: now.AddDate(0, 0, -int(pc.RetentionAuditDays))})
	if err != nil {
		return fmt.Errorf("purge du journal: %w", err)
	}
	res.AuditDeleted += n
	if !pc.PatientPurgeEnabled {
		return nil
	}
	cutoff := now.AddDate(-int(pc.RetentionPatientYears), 0, 0)
	after := uuid.Nil
	for {
		rows, err := q.ListInactivePatients(ctx, sqlcgen.ListInactivePatientsParams{PracticeID: pc.ID, AfterID: after, Before: cutoff, PageSize: scrubBatchSize})
		if err != nil {
			return fmt.Errorf("patients inactifs: %w", err)
		}
		for _, r := range rows {
			err := store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
				_, err := erasePatient(ctx, q, pc.ID, uuid.NullUUID{}, r.ID, now, false)
				return err
			})
			if errors.Is(err, domain.ErrNotFound) {
				continue // déjà anonymisé avec sa fiche principale
			}
			if err != nil {
				return fmt.Errorf("anonymisation du patient %s: %w", r.ID, err)
			}
			res.PatientsAnonymized++
		}
		if len(rows) < scrubBatchSize {
			return nil
		}
		after = rows[len(rows)-1].ID
	}
}

// erasePatient supprime ou anonymise la fiche id et celles fusionnées dedans,
// dans la transaction de q : messages effacés, adresse et motif d'annulation
// des rendez-vous effacés, lien de gestion coupé, libellés de séries vidés.
// Sans aucun rendez-vous, la fiche est supprimée pour de bon ; sinon elle est
// anonymisée (les rendez-vous restent, anonymes, pour les statistiques). Avec
// refuseFuture, des rendez-vous à venir bloquent l'opération.
func erasePatient(ctx context.Context, q *sqlcgen.Queries, practiceID uuid.UUID, user uuid.NullUUID, id uuid.UUID, now time.Time, refuseFuture bool) (string, error) {
	rows, err := q.LockPatientCluster(ctx, sqlcgen.LockPatientClusterParams{ID: id, PracticeID: practiceID})
	if err != nil {
		return "", fmt.Errorf("verrou des fiches: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	var recipients []string
	root := false
	for _, p := range rows {
		ids = append(ids, p.ID)
		if p.ID == id {
			root = !p.AnonymizedAt.Valid
		}
		if p.Phone.Valid {
			recipients = append(recipients, strings.ToLower(p.Phone.String))
		}
		if p.Email.Valid {
			recipients = append(recipients, strings.ToLower(p.Email.String))
		}
	}
	if !root {
		return "", domain.ErrNotFound
	}
	if refuseFuture {
		future, err := q.CountPatientFutureBooked(ctx, sqlcgen.CountPatientFutureBookedParams{Ids: ids, PracticeID: practiceID, Now: now})
		if err != nil {
			return "", fmt.Errorf("rendez-vous à venir: %w", err)
		}
		if future > 0 {
			return "", domain.ErrValidation{Field: "patient", Msg: "Ce patient a encore des rendez-vous à venir : annulez-les avant de supprimer sa fiche."}
		}
	}
	total, err := q.CountPatientAppointmentsAll(ctx, sqlcgen.CountPatientAppointmentsAllParams{Ids: ids, PracticeID: practiceID})
	if err != nil {
		return "", fmt.Errorf("comptage des rendez-vous: %w", err)
	}
	if _, err := q.ScrubOutboxForPatients(ctx, sqlcgen.ScrubOutboxForPatientsParams{Now: now, PracticeID: practiceID, Ids: ids, Recipients: recipients}); err != nil {
		return "", fmt.Errorf("effacement des messages: %w", err)
	}
	mode, action := "anonymized", ActionPatientAnonymized
	if total == 0 && len(ids) == 1 {
		mode, action = "deleted", ActionPatientDeleted
		if err := q.DeletePatientCarePlans(ctx, sqlcgen.DeletePatientCarePlansParams{PatientID: id, PracticeID: practiceID}); err != nil {
			return "", fmt.Errorf("suppression des séries: %w", err)
		}
		if err := q.DeletePatientRow(ctx, sqlcgen.DeletePatientRowParams{ID: id, PracticeID: practiceID}); err != nil {
			return "", fmt.Errorf("suppression du patient: %w", err)
		}
	} else {
		if _, err := q.AnonymizePatients(ctx, sqlcgen.AnonymizePatientsParams{Now: now, Ids: ids, PracticeID: practiceID}); err != nil {
			return "", fmt.Errorf("anonymisation du patient: %w", err)
		}
		if _, err := q.AnonymizePatientAppointments(ctx, sqlcgen.AnonymizePatientAppointmentsParams{Now: now, Ids: ids, PracticeID: practiceID}); err != nil {
			return "", fmt.Errorf("anonymisation des rendez-vous: %w", err)
		}
		if err := q.AnonymizeCarePlanLabels(ctx, sqlcgen.AnonymizeCarePlanLabelsParams{Now: now, Ids: ids, PracticeID: practiceID}); err != nil {
			return "", fmt.Errorf("anonymisation des séries: %w", err)
		}
	}
	if err := writeAudit(ctx, q, practiceID, user, action, "patient", id, map[string]string{
		"mode": mode, "records": fmt.Sprint(len(ids)), "appointments": fmt.Sprint(total),
	}); err != nil {
		return "", err
	}
	return mode, nil
}
