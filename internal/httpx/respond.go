package httpx

import "net/http"

// IsHTMX indique si la requête vient de htmx.
func IsHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// Error répond avec un statut et un message en texte brut (les pages d'erreur
// HTML viendront avec les templates).
func Error(w http.ResponseWriter, _ *http.Request, status int, msg string) {
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
