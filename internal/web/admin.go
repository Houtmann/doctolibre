package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"doctolibre/internal/auth"
	"doctolibre/internal/httpx"
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

	h := &adminHandlers{a: a, rd: rd, logger: logger}
	mux.HandleFunc("GET "+auth.LoginPath, h.loginForm)
	mux.Handle("POST "+auth.LoginPath, httpx.Chain(http.HandlerFunc(h.loginSubmit), byIP, byEmail))
	d.passwordRoutes(mux, rd, logger)
	require := a.RequireUser()
	mux.Handle("POST /admin/logout", require(http.HandlerFunc(h.logout)))
	mux.Handle("GET /admin", require(http.HandlerFunc(h.dashboard)))
	d.messagesRoutes(mux, rd, require)
	d.statusRoutes(mux, rd, logger)
	if d.Practice != nil {
		d.configRoutes(mux, rd, logger)
	}
	if d.Calendar != nil {
		d.calendarRoutes(mux, rd, logger)
	}
	if d.Privacy != nil {
		d.privacyRoutes(mux, rd, logger)
	}
	if d.Audit != nil {
		d.auditRoutes(mux, rd, logger)
	}
	if d.Patients != nil {
		d.patientRoutes(mux, rd, logger)
		if d.CarePlans != nil {
			d.carePlanRoutes(mux, rd, logger)
		}
	}
}

type adminHandlers struct {
	a      *auth.Service
	rd     *Renderer
	logger *slog.Logger
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
			LoginData{Next: next, Email: email, Errors: []string{errBadLoginText}})
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

func (h *adminHandlers) dashboard(w http.ResponseWriter, r *http.Request) {
	h.rd.Page(w, r, "admin_dashboard", nil)
}
