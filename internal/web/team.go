package web

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"doctolibre/internal/auth"
	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
)

// Chemins de la gestion d'équipe.
const pathTeam = "/admin/team"

// teamRoutes enregistre /admin/team (administrateur) et l'activation d'une
// invitation (publique : le futur membre n'a pas encore de compte).
func (d Deps) teamRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	h := &teamHandlers{a: d.Auth, rd: rd, logger: logger, baseURL: d.Config.BaseURL}
	require := d.Auth.RequireUser(domain.RoleAdmin)
	handle := func(pattern string, f http.HandlerFunc) { mux.Handle(pattern, require(f)) }
	handle("GET "+pathTeam, h.page)
	handle("POST "+pathTeam+"/invite", h.invite)
	handle("POST "+pathTeam+"/invitations/{id}/revoke", h.revoke)
	handle("POST "+pathTeam+"/{id}/role", h.role)
	handle("POST "+pathTeam+"/{id}/active", h.active)
	handle("POST "+pathTeam+"/practitioners/{id}/color", h.color)
	handle("POST "+pathTeam+"/delegations", h.delegate)
	handle("POST "+pathTeam+"/delegations/{id}/revoke", h.revokeDelegation)

	perWindow := rate.Every(loginWindow / loginAttempts)
	byIP := d.newLimiter(rd, logger, "invite-ip", httpx.IPKey, perWindow, loginAttempts).Middleware()
	mux.Handle("GET "+auth.AcceptPath, httpx.Chain(http.HandlerFunc(h.acceptForm), byIP))
	mux.Handle("POST "+auth.AcceptPath, httpx.Chain(http.HandlerFunc(h.acceptSubmit), byIP))
}

type teamHandlers struct {
	a       *auth.Service
	rd      *Renderer
	logger  *slog.Logger
	baseURL string
}

func (h *teamHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.rd.Error(w, r, http.StatusNotFound, "")
	case errors.Is(err, domain.ErrForbidden):
		h.rd.Error(w, r, http.StatusForbidden, "Accès refusé.")
	default:
		u, _ := auth.UserFrom(r.Context())
		httpx.LoggerFrom(r.Context()).Error("gestion d'équipe en échec",
			slog.String("user_id", u.ID.String()), slog.String("err", err.Error()))
		h.rd.Error(w, r, http.StatusInternalServerError, "")
	}
}

// RoleOption est un rôle proposé dans un sélecteur.
type RoleOption struct {
	Value, Label string
	Selected     bool
}

// TeamRow est un compte de la liste.
type TeamRow struct {
	ID, Email, Name string
	Role, RoleLabel string
	Active, Self    bool
	LastLogin       time.Time
	Roles           []RoleOption
	PractitionerID  string // vide = aucun praticien lié
	Color           string
}

// InvitationRow est une invitation en attente.
type InvitationRow struct {
	ID, Email, RoleLabel string
	ExpiresAt            time.Time
}

// DelegationRow est une délégation d'agenda en cours ou à venir.
type DelegationRow struct {
	ID, Email, Practitioner string
	From, To                time.Time
}

// DelegationOption est un choix des sélecteurs du formulaire de délégation.
type DelegationOption struct {
	Value, Label string
	Selected     bool
}

// DelegationForm est la saisie (réaffichée en cas d'erreur).
type DelegationForm struct {
	User, Practitioner, From, To string
}

// TeamPage est la donnée de la page admin_team.
type TeamPage struct {
	Delegations   []DelegationRow
	DelegUsers    []DelegationOption
	DelegAgendas  []DelegationOption
	DelegationVal DelegationForm

	CSRF        string
	Members     []TeamRow
	Invitations []InvitationRow
	Email       string
	Roles       []RoleOption // rôle de l'invitation
	Errors      []string
}

func roleOptions(selected string) []RoleOption {
	var out []RoleOption
	for _, r := range []string{domain.RolePractitioner, domain.RoleSecretary, domain.RoleAdmin} {
		out = append(out, RoleOption{Value: r, Label: auth.RoleLabel(r), Selected: r == selected})
	}
	return out
}

func (h *teamHandlers) render(w http.ResponseWriter, r *http.Request, status int, email, role string, errs []string) {
	h.renderWith(w, r, status, email, role, DelegationForm{}, errs)
}

func (h *teamHandlers) renderWith(w http.ResponseWriter, r *http.Request, status int, email, role string, df DelegationForm, errs []string) {
	u, _ := auth.UserFrom(r.Context())
	t, err := h.a.Team(r.Context(), u)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	ds, err := h.a.Delegations(r.Context(), u)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p := TeamPage{CSRF: httpx.CSRFToken(r.Context()), Email: email, Roles: roleOptions(role), Errors: errs, DelegationVal: df}
	for _, d := range ds {
		p.Delegations = append(p.Delegations, DelegationRow{ID: d.ID.String(), Email: d.Email, Practitioner: d.PractitionerName, From: d.From, To: d.To})
	}
	for _, m := range t.Members {
		if m.Active && m.Role == domain.RolePractitioner {
			p.DelegUsers = append(p.DelegUsers, DelegationOption{Value: m.ID.String(), Label: m.Email, Selected: m.ID.String() == df.User})
		}
		if m.PractitionerID != uuid.Nil {
			label := m.Name
			if label == "" {
				label = m.Email
			}
			p.DelegAgendas = append(p.DelegAgendas, DelegationOption{Value: m.PractitionerID.String(), Label: label, Selected: m.PractitionerID.String() == df.Practitioner})
		}
	}
	for _, m := range t.Members {
		p.Members = append(p.Members, TeamRow{
			ID: m.ID.String(), Email: m.Email, Name: m.Name, Role: m.Role, RoleLabel: auth.RoleLabel(m.Role),
			Active: m.Active, Self: m.ID == u.ID, LastLogin: m.LastLoginAt, Roles: roleOptions(m.Role),
		})
		if m.PractitionerID != uuid.Nil {
			last := &p.Members[len(p.Members)-1]
			last.PractitionerID, last.Color = m.PractitionerID.String(), m.Color
		}
	}
	for _, i := range t.Invitations {
		p.Invitations = append(p.Invitations, InvitationRow{ID: i.ID, Email: i.Email, RoleLabel: auth.RoleLabel(i.Role), ExpiresAt: i.ExpiresAt})
	}
	h.rd.PageStatus(w, r, status, "admin_team", p)
}

func (h *teamHandlers) page(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "", domain.RolePractitioner, nil)
}

func (h *teamHandlers) invite(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	email, role := r.PostFormValue("email"), r.PostFormValue("role")
	err := h.a.Invite(r.Context(), u, h.baseURL, email, role, httpx.ClientIP(r))
	if msgs := validation(err); msgs != nil {
		h.render(w, r, http.StatusUnprocessableEntity, email, role, msgs)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.SetFlash(w, "success", "Invitation envoyée. Le lien d'activation est valable 48 heures.")
	httpx.Redirect(w, r, pathTeam)
}

func (h *teamHandlers) delegate(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	df := DelegationForm{
		User: r.PostFormValue("user"), Practitioner: r.PostFormValue("practitioner"),
		From: r.PostFormValue("from"), To: r.PostFormValue("to"),
	}
	userID, err1 := uuid.Parse(df.User)
	practID, err2 := uuid.Parse(df.Practitioner)
	if err1 != nil || err2 != nil {
		h.renderWith(w, r, http.StatusUnprocessableEntity, "", domain.RolePractitioner, df, []string{"Choisissez le remplaçant et l'agenda."})
		return
	}
	err := h.a.CreateDelegation(r.Context(), u, auth.DelegationInput{UserID: userID, PractitionerID: practID, From: df.From, To: df.To})
	if msgs := validation(err); msgs != nil {
		h.renderWith(w, r, http.StatusUnprocessableEntity, "", domain.RolePractitioner, df, msgs)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.SetFlash(w, "success", "Délégation enregistrée.")
	httpx.Redirect(w, r, pathTeam)
}

func (h *teamHandlers) revokeDelegation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	u, _ := auth.UserFrom(r.Context())
	if err := h.a.RevokeDelegation(r.Context(), u, id); err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.SetFlash(w, "success", "Délégation révoquée.")
	httpx.Redirect(w, r, pathTeam)
}

func (h *teamHandlers) revoke(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	if err := h.a.RevokeInvitation(r.Context(), u, r.PathValue("id"), httpx.ClientIP(r)); err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.SetFlash(w, "success", "Invitation annulée.")
	httpx.Redirect(w, r, pathTeam)
}

// member exécute une action sur un compte et revient à la liste.
func (h *teamHandlers) member(w http.ResponseWriter, r *http.Request, flash string, f func(u auth.User, id uuid.UUID) error) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	u, _ := auth.UserFrom(r.Context())
	err := f(u, id)
	if msgs := validation(err); msgs != nil {
		h.render(w, r, http.StatusUnprocessableEntity, "", domain.RolePractitioner, msgs)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.SetFlash(w, "success", flash)
	httpx.Redirect(w, r, pathTeam)
}

func (h *teamHandlers) role(w http.ResponseWriter, r *http.Request) {
	h.member(w, r, "Rôle mis à jour.", func(u auth.User, id uuid.UUID) error {
		return h.a.SetRole(r.Context(), u, id, r.PostFormValue("role"))
	})
}

func (h *teamHandlers) color(w http.ResponseWriter, r *http.Request) {
	h.member(w, r, "Couleur mise à jour.", func(u auth.User, id uuid.UUID) error {
		return h.a.SetPractitionerColor(r.Context(), u, id, r.PostFormValue("color"))
	})
}

func (h *teamHandlers) active(w http.ResponseWriter, r *http.Request) {
	active := r.PostFormValue("active") == "1"
	msg := "Compte désactivé."
	if active {
		msg = "Compte réactivé."
	}
	h.member(w, r, msg, func(u auth.User, id uuid.UUID) error {
		return h.a.SetActive(r.Context(), u, id, active)
	})
}

// --- Activation d'une invitation (publique) ---

// InvitationData est la donnée de la page invitation.
type InvitationData struct {
	Token   string
	Info    auth.InvitationInfo
	Invalid bool // lien invalide, expiré ou déjà utilisé
	Errors  []string
}

func (h *teamHandlers) acceptForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	token := r.URL.Query().Get("token")
	if len(token) > maxTokenLen {
		token = ""
	}
	info, err := h.a.Invitation(r.Context(), token)
	switch {
	case errors.Is(err, auth.ErrInvalidInvitation):
		h.rd.PageStatus(w, r, http.StatusBadRequest, "invitation", InvitationData{Invalid: true})
		return
	case err != nil:
		h.fail(w, r, err)
		return
	}
	h.rd.Page(w, r, "invitation", InvitationData{Token: token, Info: info})
}

func (h *teamHandlers) acceptSubmit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	token := r.PostFormValue("token")
	pw, pw2 := r.PostFormValue("password"), r.PostFormValue("password2")
	if len(token) > maxTokenLen {
		token = ""
	}
	info, err := h.a.Invitation(r.Context(), token)
	if errors.Is(err, auth.ErrInvalidInvitation) {
		h.rd.PageStatus(w, r, http.StatusBadRequest, "invitation", InvitationData{Invalid: true})
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	invalid := func(msgs ...string) {
		h.rd.PageStatus(w, r, http.StatusUnprocessableEntity, "invitation", InvitationData{Token: token, Info: info, Errors: msgs})
	}
	if len(pw) > maxPasswordLen {
		invalid("Mot de passe trop long.")
		return
	}
	if pw != pw2 {
		invalid("Les deux mots de passe ne sont pas identiques.")
		return
	}
	err = h.a.AcceptInvitation(r.Context(), token, pw, httpx.ClientIP(r))
	var verr domain.ErrValidation
	switch {
	case errors.Is(err, auth.ErrInvalidInvitation):
		h.rd.PageStatus(w, r, http.StatusBadRequest, "invitation", InvitationData{Invalid: true})
		return
	case errors.As(err, &verr):
		if verr.Field == "password" {
			invalid("Mot de passe : " + verr.Msg + ".")
		} else {
			invalid(verr.Msg)
		}
		return
	case err != nil:
		h.fail(w, r, err)
		return
	}
	h.rd.SetFlash(w, "success", "Votre compte est activé. Vous pouvez vous connecter.")
	httpx.Redirect(w, r, auth.LoginPath)
}
