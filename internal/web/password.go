package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"golang.org/x/time/rate"

	"doctolibre/internal/auth"
	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
)

const (
	maxTokenLen = 128
	flashForgot = "Si un compte correspond à cette adresse, un email contenant un lien valable 1 heure vient d'être envoyé."
	flashReset  = "Votre mot de passe a été changé. Vous pouvez vous connecter."
)

// PasswordResetData est la donnée de la page « nouveau mot de passe ».
type PasswordResetData struct {
	Token   string
	Invalid bool // lien invalide, expiré ou déjà utilisé
	Errors  []string
}

type passwordHandlers struct {
	a       *auth.Service
	rd      *Renderer
	logger  *slog.Logger
	baseURL string
}

// passwordRoutes enregistre la réinitialisation du mot de passe (publique :
// l'utilisateur n'a plus accès à son compte).
func (d Deps) passwordRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	h := &passwordHandlers{a: d.Auth, rd: rd, logger: logger, baseURL: d.Config.BaseURL}
	perWindow := rate.Every(loginWindow / loginAttempts)
	byIP := httpx.RateLimit(httpx.ClientIP, perWindow, loginAttempts)
	byEmail := httpx.RateLimit(func(r *http.Request) string {
		k := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
		if len(k) > maxEmailKeyLen {
			k = k[:maxEmailKeyLen]
		}
		return k
	}, perWindow, loginAttempts)

	mux.HandleFunc("GET "+auth.ForgotPath, h.forgotForm)
	mux.Handle("POST "+auth.ForgotPath, httpx.Chain(http.HandlerFunc(h.forgotSubmit), byIP, byEmail))
	mux.HandleFunc("GET "+auth.ResetPath, h.resetForm)
	mux.Handle("POST "+auth.ResetPath, httpx.Chain(http.HandlerFunc(h.resetSubmit), byIP))
}

func (h *passwordHandlers) forgotForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	h.rd.Page(w, r, "password_forgot", nil)
}

func (h *passwordHandlers) forgotSubmit(w http.ResponseWriter, r *http.Request) {
	err := h.a.RequestPasswordReset(r.Context(), h.baseURL, r.PostFormValue("email"), httpx.ClientIP(r))
	if err != nil {
		httpx.LoggerFrom(r.Context()).Error("demande de réinitialisation en échec", slog.String("err", err.Error()))
		httpx.Error(w, r, http.StatusInternalServerError, "erreur interne")
		return
	}
	// Même réponse que le compte existe ou non.
	h.rd.SetFlash(w, "success", flashForgot)
	httpx.Redirect(w, r, auth.LoginPath)
}

func (h *passwordHandlers) resetForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	token := r.URL.Query().Get("token")
	if len(token) > maxTokenLen {
		token = ""
	}
	ok, err := h.a.ResetTokenValid(r.Context(), token)
	if err != nil {
		httpx.LoggerFrom(r.Context()).Error("lecture du jeton en échec", slog.String("err", err.Error()))
		httpx.Error(w, r, http.StatusInternalServerError, "erreur interne")
		return
	}
	if !ok {
		h.rd.PageStatus(w, r, http.StatusBadRequest, "password_reset", PasswordResetData{Invalid: true})
		return
	}
	h.rd.Page(w, r, "password_reset", PasswordResetData{Token: token})
}

func (h *passwordHandlers) resetSubmit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	token := r.PostFormValue("token")
	pw, pw2 := r.PostFormValue("password"), r.PostFormValue("password2")
	invalid := func(msgs ...string) {
		h.rd.PageStatus(w, r, http.StatusUnprocessableEntity, "password_reset", PasswordResetData{Token: token, Errors: msgs})
	}
	if len(pw) > maxPasswordLen || len(token) > maxTokenLen {
		invalid("Mot de passe trop long.")
		return
	}
	if pw != pw2 {
		invalid("Les deux mots de passe ne sont pas identiques.")
		return
	}
	err := h.a.ResetPassword(r.Context(), token, pw, httpx.ClientIP(r))
	var verr domain.ErrValidation
	switch {
	case errors.Is(err, auth.ErrInvalidResetToken):
		h.rd.PageStatus(w, r, http.StatusBadRequest, "password_reset", PasswordResetData{Invalid: true})
		return
	case errors.As(err, &verr):
		invalid("Mot de passe : " + verr.Msg + ".")
		return
	case err != nil:
		httpx.LoggerFrom(r.Context()).Error("réinitialisation en échec", slog.String("err", err.Error()))
		httpx.Error(w, r, http.StatusInternalServerError, "erreur interne")
		return
	}
	h.rd.SetFlash(w, "success", flashReset)
	httpx.Redirect(w, r, auth.LoginPath)
}
