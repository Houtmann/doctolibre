package web

import (
	"errors"
	"log/slog"
	"net/http"

	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/notify"
	"doctolibre/internal/service"
)

// Chemin de la page des fournisseurs d'email et de SMS du cabinet (T086).
const pathTransport = "/admin/settings/notifications/transport"

// Champs communs à tous les fournisseurs SMS (préfixe « f_common_ » dans le formulaire).
var commonTransportKeys = map[string]bool{"sender": true, "transliterate": true}

type transportHandlers struct {
	svc    *service.NotifyTransport
	rd     *Renderer
	logger *slog.Logger
}

// TransportPage est la donnée de la page admin_transport.
type TransportPage struct {
	Email, SMS service.TransportView
	// Errors : erreurs du formulaire du canal ErrorKind ("email" | "sms").
	Errors    []string
	ErrorKind string
}

func (d Deps) transportRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	h := &transportHandlers{svc: d.NotifyTransport, rd: rd, logger: logger}
	require := d.Auth.RequireUser(domain.RoleAdmin, domain.RolePractitioner)
	mux.Handle("GET "+pathTransport, require(http.HandlerFunc(h.page)))
	mux.Handle("POST "+pathTransport+"/{kind}", require(http.HandlerFunc(h.save)))
	mux.Handle("POST "+pathTransport+"/{kind}/test", require(http.HandlerFunc(h.test)))
}

func (h *transportHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.rd.Error(w, r, http.StatusNotFound, "")
	case errors.Is(err, domain.ErrForbidden):
		h.rd.Error(w, r, http.StatusForbidden, "Accès refusé.")
	default:
		a := actorFrom(r)
		httpx.LoggerFrom(r.Context()).Error("fournisseurs de notification en échec",
			slog.String("user_id", a.UserID.String()), slog.String("err", err.Error()))
		h.rd.Error(w, r, http.StatusInternalServerError, "")
	}
}

func (h *transportHandlers) page(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.View(r.Context(), actorFrom(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	h.rd.Page(w, r, "admin_transport", TransportPage{Email: v.Email, SMS: v.SMS})
}

// kindOf valide le canal du chemin.
func kindOf(r *http.Request) (string, bool) {
	k := r.PathValue("kind")
	return k, k == notify.KindEmail || k == notify.KindSMS
}

// readTransportInput lit le formulaire d'un canal : seuls les champs du
// fournisseur choisi sont lus (tous les blocs sont rendus, pour fonctionner sans JS).
func readTransportInput(r *http.Request, kind string) service.TransportInput {
	in := service.TransportInput{
		Kind: kind, Mode: r.PostFormValue("mode"), Driver: r.PostFormValue("driver"),
		Settings: map[string]string{}, Secrets: map[string]string{}, Clear: map[string]bool{},
	}
	if kind == notify.KindEmail {
		in.Driver = "smtp"
	}
	fields, _ := notify.FieldsOf(kind, in.Driver)
	for _, f := range fields {
		prefix := in.Driver
		if commonTransportKeys[f.Key] {
			prefix = "common"
		}
		switch {
		case f.Key == "transliterate":
			in.Settings[f.Key] = "false"
			if r.PostFormValue("f_common_transliterate") == "true" {
				in.Settings[f.Key] = "true"
			}
		case f.Secret:
			in.Secrets[f.Key] = r.PostFormValue("x_" + prefix + "_" + f.Key)
			in.Clear[f.Key] = r.PostFormValue("c_"+prefix+"_"+f.Key) == "1"
		default:
			in.Settings[f.Key] = r.PostFormValue("f_" + prefix + "_" + f.Key)
		}
	}
	return in
}

func (h *transportHandlers) save(w http.ResponseWriter, r *http.Request) {
	kind, ok := kindOf(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	a := actorFrom(r)
	in := readTransportInput(r, kind)
	res, err := h.svc.Update(r.Context(), a, in)
	if msgs := validation(err); msgs != nil {
		v, verr := h.svc.View(r.Context(), a)
		if verr != nil {
			h.fail(w, r, verr)
			return
		}
		// Saisie conservée (sans secrets) pour la réafficher.
		edit := &v.Email
		if kind == notify.KindSMS {
			edit = &v.SMS
		}
		edit.Mode, edit.Driver, edit.Settings = in.Mode, in.Driver, in.Settings
		w.Header().Set("Cache-Control", "no-store")
		h.rd.PageStatus(w, r, http.StatusUnprocessableEntity, "admin_transport", TransportPage{Email: v.Email, SMS: v.SMS, Errors: msgs, ErrorKind: kind})
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if res.SettingsAdjusted {
		h.rd.SetFlash(w, "warning", "Fournisseur enregistré. Les réglages de notification qui utilisaient ce canal ont été ramenés à l'autre canal ou à « Aucun » : vérifiez-les.")
	} else {
		h.rd.SetFlash(w, "success", "Fournisseur enregistré. Il s'applique dès le prochain message.")
	}
	httpx.Redirect(w, r, pathTransport)
}

func (h *transportHandlers) test(w http.ResponseWriter, r *http.Request) {
	kind, ok := kindOf(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	err := h.svc.SendTest(r.Context(), actorFrom(r), kind, r.PostFormValue("phone"))
	var te service.TestError
	switch {
	case err == nil:
		h.rd.SetFlash(w, "success", "Message de test envoyé.")
	case errors.Is(err, service.ErrTestRateLimited):
		h.rd.SetFlash(w, "error", "Trop de messages de test récemment : réessayez dans une heure.")
	case errors.Is(err, service.ErrChannelDisabled):
		h.rd.SetFlash(w, "error", "Ce canal est désactivé.")
	case errors.Is(err, notify.ErrUnreadable):
		h.rd.SetFlash(w, "error", "Les identifiants enregistrés sont illisibles : ressaisissez-les.")
	case errors.As(err, &te):
		h.rd.SetFlash(w, "error", "L'envoi de test a échoué. Vérifiez les réglages enregistrés (référence "+te.LogID+").")
	default:
		if msgs := validation(err); msgs != nil {
			h.rd.SetFlash(w, "error", msgs[0])
		} else {
			h.fail(w, r, err)
			return
		}
	}
	httpx.Redirect(w, r, pathTransport)
}
