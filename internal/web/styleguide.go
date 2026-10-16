package web

import "net/http"

// Données factices de la page /dev/styleguide (aucun accès base de données).

type styleguideRow struct {
	Time, Name, Status, StatusName string
}

type styleguideData struct {
	Errors  []string
	Flashes []Flash
	Rows    []styleguideRow
}

func styleguideView() styleguideData {
	return styleguideData{
		Errors: []string{"Le nom est obligatoire.", "Un téléphone ou un email est requis."},
		Flashes: []Flash{
			{Kind: "success", Message: "Enregistré."}, {Kind: "error", Message: "Une erreur est survenue."},
			{Kind: "info", Message: "Information neutre."}, {Kind: "warning", Message: "Attention, à vérifier."},
		},
		Rows: []styleguideRow{
			{"09:00", "Camille Martin", "booked", "Réservé"},
			{"09:30", "Jean Dupont", "done", "Honoré"},
			{"10:00", "Léa Bernard", "no_show", "Absent"},
			{"10:30", "Paul Durand", "cancelled", "Annulé"},
		},
	}
}

// styleguide rend la page de contrôle du design system (montée en dev seulement).
func (r *Renderer) styleguide(w http.ResponseWriter, req *http.Request) {
	r.Page(w, req, "dev_styleguide", styleguideView())
}
