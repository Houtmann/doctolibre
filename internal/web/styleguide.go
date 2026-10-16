package web

import (
	"net/http"
	"time"
)

// Données factices de la page /dev/styleguide (aucun accès base de données).

type swatch struct {
	Name  string // nom du jeton, sans le préfixe --app-
	Class string // classe CSS portant les couleurs (la CSP interdit style=)
}

type styleguideAppt struct {
	Col, Span  int
	Status     string
	StatusName string
	Time       string
	Patient    string
	Head       string
}

type styleguidePatient struct {
	ID       string
	Name     string
	Contact  string
	Selected bool
}

type styleguideData struct {
	Swatches  []swatch
	Errors    []string
	Form      PatientForm
	Appts     []styleguideAppt
	Results   map[string]any
	StatusBtn map[string]any
	Summary   Summary
	Flashes   []Flash
}

func styleguideView() styleguideData {
	loc := time.FixedZone("CET", 3600)
	return styleguideData{
		Swatches: []swatch{
			{"success-bg / -fg", "sw-success"}, {"error-bg / -fg", "sw-error"},
			{"info-bg / -fg", "sw-info"}, {"warning-bg / -fg", "sw-warning"},
			{"st-booked-*", "sw-st-booked"}, {"st-done-*", "sw-st-done"},
			{"st-no_show-*", "sw-st-no_show"}, {"st-cancelled-*", "sw-st-cancelled"},
			{"band-a / band-b", "sw-band"}, {"now", "sw-now"},
		},
		Errors: []string{"Le nom est obligatoire.", "Un téléphone ou un email est requis."},
		Form:   PatientForm{FirstName: "Camille", LastName: "Martin", Phone: "0600000000", Email: "camille@example.test", SMSOptIn: "1"},
		Appts: []styleguideAppt{
			{Col: 2, Span: 2, Status: "booked", StatusName: "à venir", Time: "09h00", Patient: "Camille Martin", Head: "Lun."},
			{Col: 3, Span: 2, Status: "done", StatusName: "honoré", Time: "09h00", Patient: "Jean Dupont", Head: "Mar."},
			{Col: 4, Span: 2, Status: "no_show", StatusName: "absent", Time: "09h00", Patient: "Léa Bernard", Head: "Mer."},
			{Col: 5, Span: 2, Status: "cancelled", StatusName: "annulé", Time: "09h00", Patient: "Paul Durand", Head: "Jeu."},
		},
		Results: map[string]any{
			"Patients": []styleguidePatient{
				{ID: "p1", Name: "Camille Martin", Contact: "06 00 00 00 00", Selected: true},
				{ID: "p2", Name: "Jean Dupont", Contact: "jean@example.test"},
			},
			"Form": map[string]any{"Query": "ma"},
		},
		StatusBtn: map[string]any{
			"CSRF": "", "View": "week", "Status": "done", "Label": "Marquer honoré",
			"D": map[string]any{"ID": "00000000-0000-0000-0000-000000000000", "MoveDate": "2026-10-15"},
		},
		Summary: Summary{
			Practice: "Cabinet de démonstration", Practitioner: "Dr Exemple", Type: "Séance de kinésithérapie",
			Duration: 30 * time.Minute, Start: time.Date(2026, 10, 15, 9, 0, 0, 0, loc),
		},
		Flashes: []Flash{
			{Kind: "success", Message: "Enregistré."}, {Kind: "error", Message: "Une erreur est survenue."},
			{Kind: "info", Message: "Information neutre."}, {Kind: "warning", Message: "Attention, à vérifier."},
		},
	}
}

// styleguide rend la page de contrôle du design system (montée en dev seulement).
func (r *Renderer) styleguide(w http.ResponseWriter, req *http.Request) {
	r.Page(w, req, "dev_styleguide", styleguideView())
}
