package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"doctolibre/internal/auth"
	"doctolibre/internal/httpx"
)

// Paramètre (query ou formulaire) qui désigne le praticien visé par une page ou
// une action d'agenda ou d'horaires.
const practitionerParam = "practitioner"

type targetKey struct{}

// targetFrom retourne le praticien visé placé par withTarget (uuid.Nil = le sien).
func targetFrom(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(targetKey{}).(uuid.UUID)
	return id
}

// withTarget lit le paramètre « practitioner » (s'il est présent), vérifie dans
// la matrice de permissions (auth.Can) que l'utilisateur peut effectuer action
// sur l'agenda de ce praticien, puis le place dans le contexte pour actorFrom.
// Sans paramètre, la permission est évaluée sur « son propre agenda ».
func withTarget(rd *Renderer, action auth.Action) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, _ := auth.UserFrom(r.Context())
			var id uuid.UUID
			if raw := strings.TrimSpace(r.FormValue(practitionerParam)); raw != "" {
				var err error
				if id, err = uuid.Parse(raw); err != nil {
					rd.Error(w, r, http.StatusNotFound, "")
					return
				}
			}
			if !auth.Can(u, action, auth.Resource{PracticeID: u.PracticeID, PractitionerID: id}) {
				httpx.LoggerFrom(r.Context()).Warn("accès refusé par la matrice de permissions",
					"user_id", u.ID.String(), "action", string(action))
				rd.Error(w, r, http.StatusForbidden, "Accès refusé.")
				return
			}
			if id != uuid.Nil {
				r = r.WithContext(context.WithValue(r.Context(), targetKey{}, id))
			}
			next.ServeHTTP(w, r)
		})
	}
}
