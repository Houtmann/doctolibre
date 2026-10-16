package web

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/google/uuid"

	"doctolibre/internal/service"
)

// firstPath : « premier créneau disponible » d'un cabinet, tous praticiens confondus.
// Le segment est réservé : un praticien ne peut pas avoir ce slug.
const firstPath = "/p/{practice}/premier-disponible"

// firstTypes liste les motifs proposés par « premier disponible » : seulement
// quand au moins deux praticiens reçoivent en ligne (sinon la page praticien suffit).
func (h *publicHandlers) firstTypes(r *http.Request, practiceSlug string) ([]TypeView, error) {
	types, n, err := h.svc.PracticeTypes(r.Context(), practiceSlug)
	if err != nil {
		return nil, err
	}
	if n < 2 {
		return nil, nil
	}
	base := "/p/" + url.PathEscape(practiceSlug) + "/premier-disponible"
	out := make([]TypeView, 0, len(types))
	for _, t := range types {
		out = append(out, TypeView{
			ID: t.ID.String(), Name: t.Name, Duration: typeDuration(t),
			URL: base + "?" + url.Values{"type": {t.ID.String()}}.Encode(),
		})
	}
	return out, nil
}

// firstAvailable redirige vers le formulaire de réservation du créneau le plus
// proche, tous praticiens confondus, pour le motif demandé.
func (h *publicHandlers) firstAvailable(w http.ResponseWriter, r *http.Request) {
	practiceSlug := r.PathValue("practice")
	home := "/p/" + url.PathEscape(practiceSlug)
	typeID, err := uuid.Parse(r.URL.Query().Get("type"))
	if err != nil {
		http.Redirect(w, r, home, http.StatusSeeOther)
		return
	}
	first, err := h.svc.FirstAvailable(r.Context(), practiceSlug, typeID)
	switch {
	case errors.Is(err, service.ErrNoAvailability):
		h.rd.SetFlash(w, "error", "Aucun créneau n'est disponible pour ce motif pour le moment. Appelez le cabinet ou choisissez un praticien.")
		http.Redirect(w, r, home, http.StatusSeeOther)
		return
	case err != nil:
		h.fail(w, r, err)
		return
	}
	dest := home + "/" + url.PathEscape(first.Practitioner.Slug) + "/book/details?" + url.Values{
		"type": {first.Type.ID.String()}, "start": {first.Start.UTC().Format("2006-01-02T15:04:05Z")},
	}.Encode()
	http.Redirect(w, r, dest, http.StatusSeeOther)
}
