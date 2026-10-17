package httpx

import "net/http"

// IsHTMX indique si la requête vient de htmx.
func IsHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// Error répond avec un statut et un message en texte brut (les pages d'erreur
// HTML viendront avec les templates).
//
// Une requête htmx refusée en 429 reçoit un fragment (alerte à insérer) et non du texte brut.
func Error(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if status == http.StatusTooManyRequests && r != nil && IsHTMX(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(tooManyFragment(msg))) //nolint:gosec // msg est échappé par tooManyFragment
		return
	}
	http.Error(w, msg, status)
}

// Redirect redirige en 303 ; pour une requête htmx, utilise HX-Redirect
// (navigation complète côté client) au lieu d'un redirect que XHR suivrait.
func Redirect(w http.ResponseWriter, r *http.Request, url string) {
	if IsHTMX(r) {
		w.Header().Set("HX-Redirect", url)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}
