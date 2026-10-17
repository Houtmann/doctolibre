package web

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"doctolibre/internal/auth"
	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/service"
)

// Limite de tentatives de connexion : 5 par fenêtre de 15 minutes, par IP et par email.
const (
	loginAttempts = 5
	loginWindow   = 15 * time.Minute

	maxEmailKeyLen  = 254
	maxPasswordLen  = 1024
	errBadLoginText = "Adresse email ou mot de passe incorrect."
)

// LoginData est la donnée de la page login.
type LoginData struct {
	Next   string
	Email  string
	Errors []string
	Failed bool // identifiants refusés : les deux champs sont marqués en erreur
}

func (d Deps) adminRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	a := d.Auth
	perWindow := rate.Every(loginWindow / loginAttempts)
	byIP := httpx.RateLimit(httpx.ClientIP, perWindow, loginAttempts)
	byEmail := httpx.RateLimit(func(r *http.Request) string {
		k := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
		if len(k) > maxEmailKeyLen {
			k = k[:maxEmailKeyLen]
		}
		return k
	}, perWindow, loginAttempts)

	h := &adminHandlers{a: a, rd: rd, logger: logger, practice: d.Practice, baseURL: d.Config.BaseURL}
	mux.HandleFunc("GET "+auth.LoginPath, h.loginForm)
	mux.Handle("POST "+auth.LoginPath, httpx.Chain(http.HandlerFunc(h.loginSubmit), byIP, byEmail))
	d.passwordRoutes(mux, rd, logger)
	require := a.RequireUser()
	mux.Handle("POST /admin/logout", require(http.HandlerFunc(h.logout)))
	mux.Handle("GET /admin", require(http.HandlerFunc(h.dashboard)))
	d.messagesRoutes(mux, rd, require)
	d.statusRoutes(mux, rd, logger)
	d.teamRoutes(mux, rd, logger)
	if d.Practice != nil {
		d.configRoutes(mux, rd, logger)
	}
	if d.NotifyTransport != nil {
		d.transportRoutes(mux, rd, logger)
	}
	if d.Calendar != nil {
		d.calendarRoutes(mux, rd, logger)
		d.todayRoutes(mux, rd, logger)
	}
	if d.Privacy != nil {
		d.privacyRoutes(mux, rd, logger)
	}
	if d.Audit != nil {
		d.auditRoutes(mux, rd, logger)
	}
	if d.Waitlist != nil {
		d.waitlistAdminRoutes(mux, rd, logger)
	}
	if d.Patients != nil {
		d.patientRoutes(mux, rd, logger)
		if d.PatientImport != nil {
			d.patientImportRoutes(mux, rd, logger)
		}
		if d.CarePlans != nil {
			d.carePlanRoutes(mux, rd, logger)
		}
	}
}

type adminHandlers struct {
	a      *auth.Service
	rd     *Renderer
	logger *slog.Logger

	practice *service.Practice // optionnel : sans lui, le tableau de bord n'affiche pas les liens de réservation
	baseURL  string
}

func (h *adminHandlers) loginForm(w http.ResponseWriter, r *http.Request) {
	next := auth.SafeNext(r.URL.Query().Get("next"))
	if sess, err := h.a.Authenticate(r.Context(), h.a.TokenFrom(r)); err == nil {
		if sess.Refreshed {
			h.a.SetCookie(w, h.a.TokenFrom(r), sess.ExpiresAt)
		}
		httpx.Redirect(w, r, next)
		return
	}
	h.rd.Page(w, r, "login", LoginData{Next: next})
}

func (h *adminHandlers) loginSubmit(w http.ResponseWriter, r *http.Request) {
	next := auth.SafeNext(r.PostFormValue("next"))
	email := r.PostFormValue("email")
	password := r.PostFormValue("password")
	fail := func() {
		h.rd.PageStatus(w, r, http.StatusUnauthorized, "login",
			LoginData{Next: next, Email: email, Errors: []string{errBadLoginText}, Failed: true})
	}
	if len(password) > maxPasswordLen {
		fail()
		return
	}
	sess, err := h.a.Login(r.Context(), auth.LoginParams{
		Email: email, Password: password,
		OldToken:  h.a.TokenFrom(r),
		IP:        httpx.ClientIP(r),
		UserAgent: r.UserAgent(),
	})
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		fail()
		return
	case err != nil:
		httpx.LoggerFrom(r.Context()).Error("connexion en échec", slog.String("err", err.Error()))
		httpx.Error(w, r, http.StatusInternalServerError, "erreur interne")
		return
	}
	h.a.SetCookie(w, sess.Token, sess.ExpiresAt)
	httpx.Redirect(w, r, next)
}

func (h *adminHandlers) logout(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	if err := h.a.Logout(r.Context(), h.a.TokenFrom(r), u, httpx.ClientIP(r)); err != nil {
		httpx.LoggerFrom(r.Context()).Error("déconnexion en échec", slog.String("user_id", u.ID.String()), slog.String("err", err.Error()))
		httpx.Error(w, r, http.StatusInternalServerError, "erreur interne")
		return
	}
	h.a.ClearCookie(w)
	h.rd.SetFlash(w, "success", "Vous êtes déconnecté.")
	httpx.Redirect(w, r, auth.LoginPath)
}

// DashboardData est la donnée de la page tableau de bord.
type DashboardData struct {
	// Booking est nil quand le service de configuration n'est pas branché.
	Booking *DashboardBooking
	// EmailDisabled, SMSDisabled : canal désactivé par le cabinet (T086) => bandeau d'avertissement.
	EmailDisabled, SMSDisabled bool
}

// DashboardBooking décrit la page de réservation publique du cabinet.
type DashboardBooking struct {
	PracticeURL                string
	Practitioners              []DashboardPractitioner
	NeedAvailability, NeedType bool
	CanConfigure               bool
}

// DashboardPractitioner est un praticien actif et son adresse de réservation.
type DashboardPractitioner struct {
	Name, URL string
}

func (h *adminHandlers) dashboard(w http.ResponseWriter, r *http.Request) {
	data := DashboardData{}
	if h.practice != nil {
		a := actorFrom(r)
		d, err := h.practice.Dashboard(r.Context(), a)
		if err != nil {
			httpx.LoggerFrom(r.Context()).Error("tableau de bord en échec", slog.String("practice_id", a.PracticeID.String()), slog.String("err", err.Error()))
			h.rd.Error(w, r, http.StatusInternalServerError, "")
			return
		}
		base := strings.TrimRight(h.baseURL, "/")
		if base == "" {
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			base = scheme + "://" + r.Host
		}
		b := &DashboardBooking{
			PracticeURL:      base + "/p/" + url.PathEscape(d.PracticeSlug),
			NeedAvailability: !d.HasAvailability,
			NeedType:         !d.HasBookableType,
			CanConfigure:     a.Role != domain.RoleSecretary,
		}
		for _, p := range d.Practitioners {
			b.Practitioners = append(b.Practitioners, DashboardPractitioner{
				Name: strings.TrimSpace(p.FirstName + " " + p.LastName),
				URL:  b.PracticeURL + "/" + url.PathEscape(p.Slug),
			})
		}
		data.Booking = b
		ch, err := h.practice.Channels(r.Context(), a)
		if err != nil {
			httpx.LoggerFrom(r.Context()).Error("canaux de notification en échec", slog.String("practice_id", a.PracticeID.String()), slog.String("err", err.Error()))
			h.rd.Error(w, r, http.StatusInternalServerError, "")
			return
		}
		data.EmailDisabled, data.SMSDisabled = !ch.Email, !ch.SMS
	}
	h.rd.Page(w, r, "admin_dashboard", data)
}
