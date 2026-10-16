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

// Données factices de la page /dev/styleguide/v2 (design system « Pétrole & papier »).
// Aucun accès base de données ; les classes de mise en page sont des .dl-sg-* (components.css).

type sgToken struct {
	Name  string // nom du jeton sans le préfixe --dl-
	Label string
}

type sgTokenGroup struct {
	Title  string
	Tokens []sgToken
}

type sgBox struct {
	Kind  string // classe .dl-sg-box--<Kind>
	Label string
}

type sgSpace struct {
	N  int
	Px string
}

type sgChoice struct{ Title, Meta string }

type sgSlotDay struct {
	Title   string
	Holiday bool
	Slots   []sgSlot
}

type sgSlot struct {
	Time    string
	Current bool
}

type sgCalHour struct {
	Row   int
	Label string
}

type sgCalAppt struct {
	Row, Span, Col int
	Status         string
	Past           bool
	Time, Name     string
	Kind           string
}

type sgCalHead struct {
	Col         int
	Label, Note string
	Today       bool
}

type sgListAppt struct {
	Status, StatusName, Range, Name, Kind string
}

type sgPatientRow struct {
	Name, Contact, Next string
	Status, StatusName  string
	Inactive            bool
}

type styleguideV2Data struct {
	Primitives []sgTokenGroup
	Semantic   []sgToken
	Boxes      []sgBox
	States     []sgBox
	Spaces     []sgSpace
	Radii      []string
	Choices    []sgChoice
	SlotDays   []sgSlotDay
	CalHours   []sgCalHour
	CalHeads   []sgCalHead
	CalAppts   []sgCalAppt
	ListAppts  []sgListAppt
	Patients   []sgPatientRow
	NavItems   []string
}

func styleguideV2View() styleguideV2Data {
	prim := func(family string, steps ...string) []sgToken {
		out := make([]sgToken, len(steps))
		for i, s := range steps {
			out[i] = sgToken{Name: "p-" + family + "-" + s, Label: s}
		}
		return out
	}
	return styleguideV2Data{
		Primitives: []sgTokenGroup{
			{"Pétrole — marque", prim("petrol", "50", "100", "300", "400", "600", "700", "800", "900", "950")},
			{"Pierre — neutres clairs", prim("stone", "0", "50", "100", "150", "200", "400", "600", "700", "900")},
			{"Encre — neutres sombres", prim("ink", "950", "900", "850", "800", "700", "500", "300", "100")},
		},
		Semantic: []sgToken{
			{"bg", "Fond"}, {"surface", "Surface"}, {"surface-2", "Surface 2"}, {"border", "Bordure"},
			{"border-strong", "Bordure forte"}, {"text", "Texte"}, {"text-muted", "Texte atténué"},
			{"primary", "Primaire"}, {"primary-hover", "Primaire survol"}, {"primary-soft", "Primaire doux"},
			{"danger", "Danger"}, {"focus", "Focus"},
		},
		Boxes: []sgBox{{"success", "Succès"}, {"error", "Erreur"}, {"info", "Info"}, {"warning", "Attention"}},
		States: []sgBox{
			{"booked", "Réservé"}, {"done", "Honoré"}, {"no_show", "Absent"}, {"cancelled", "Annulé"},
		},
		Spaces: []sgSpace{{1, "4"}, {2, "8"}, {3, "12"}, {4, "16"}, {5, "24"}, {6, "32"}, {7, "48"}, {8, "64"}},
		Radii:  []string{"sm", "md", "lg", "pill"},
		Choices: []sgChoice{
			{"Bilan initial", "45 min"}, {"Séance de rééducation", "30 min"},
		},
		SlotDays: []sgSlotDay{
			{Title: "Lundi 13", Slots: []sgSlot{{"08:30", false}, {"09:00", false}, {"10:30", true}, {"14:00", false}}},
			{Title: "Mardi 14", Holiday: true},
		},
		CalHours: []sgCalHour{
			{2, "08:00"}, {6, "09:00"}, {10, "10:00"}, {14, "11:00"}, {18, "12:00"}, {22, "13:00"}, {26, "14:00"}, {30, "15:00"},
		},
		CalHeads: []sgCalHead{
			{Col: 2, Label: "Lun 13"}, {Col: 3, Label: "Mar 14", Note: "Férié"}, {Col: 4, Label: "Mer 15"},
			{Col: 5, Label: "Jeu 16", Today: true}, {Col: 6, Label: "Ven 17"},
		},
		CalAppts: []sgCalAppt{
			{Row: 2, Span: 2, Col: 2, Status: "done", Past: true, Time: "08:00", Name: "A. Bernard", Kind: "Bilan"},
			{Row: 4, Span: 2, Col: 2, Status: "done", Past: true, Time: "08:30", Name: "L. Petit", Kind: "Rééducation"},
			{Row: 8, Span: 3, Col: 4, Status: "no_show", Past: true, Time: "09:30", Name: "M. Durand", Kind: "Bilan initial"},
			{Row: 12, Span: 2, Col: 4, Status: "cancelled", Time: "10:30", Name: "S. Moreau", Kind: "Rééducation"},
			{Row: 3, Span: 2, Col: 5, Status: "booked", Time: "08:15", Name: "J. Roux", Kind: "Rééducation"},
			{Row: 10, Span: 2, Col: 5, Status: "booked", Time: "10:00", Name: "C. Leroy", Kind: "Rééducation ⌂"},
			{Row: 22, Span: 2, Col: 5, Status: "booked", Time: "13:00", Name: "P. Fournier", Kind: "Rééducation"},
			{Row: 6, Span: 2, Col: 6, Status: "booked", Time: "09:00", Name: "E. Girard", Kind: "Rééducation"},
			{Row: 26, Span: 3, Col: 6, Status: "booked", Time: "14:00", Name: "N. Lambert", Kind: "Bilan initial"},
		},
		ListAppts: []sgListAppt{
			{"booked", "", "09:00–09:30", "E. Girard", "Rééducation"},
			{"done", "honoré", "08:30–09:00", "L. Petit", "Rééducation"},
			{"no_show", "absent", "09:30–10:15", "M. Durand", "Bilan"},
			{"cancelled", "annulé", "10:30–11:00", "S. Moreau", "Rééducation"},
		},
		Patients: []sgPatientRow{
			{Name: "Durand Marc", Contact: "06 •• •• •• 12", Next: "16/10 09:30", Status: "booked", StatusName: "Réservé"},
			{Name: "Petit Léa", Contact: "lea@exemple.fr", Next: "—"},
			{Name: "Ancien patient", Contact: "—", Next: "—", Inactive: true},
		},
		NavItems: []string{"Agenda", "Patients", "Liste d'attente", "Horaires", "Paramètres"},
	}
}

// styleguideV2 rend la page de contrôle du design system v2 (montée en dev seulement).
func (r *Renderer) styleguideV2(w http.ResponseWriter, req *http.Request) {
	r.Page(w, req, "dev_styleguide_v2", styleguideV2View())
}
