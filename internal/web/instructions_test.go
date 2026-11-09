package web_test

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"doctolibre/internal/domain"
	"doctolibre/internal/store/sqlcgen"
)

const instrText = "Apporter l'ordonnance\nTenue souple <script>alert(1)</script>"

// T070 : consignes affichées avant la réservation, case « J'ai lu » vérifiée côté serveur,
// reprises sur la confirmation et dans l'ICS patient.
func TestPublicBookingInstructions(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	if err := sqlcgen.New(e.db).SetAppointmentTypeInstructions(context.Background(), sqlcgen.SetAppointmentTypeInstructionsParams{
		ID: e.typ.ID, PracticeID: e.pc.ID, PatientInstructions: instrText, InstructionsAckRequired: true,
	}); err != nil {
		t.Fatal(err)
	}
	c := newClient(t, e.h, "192.0.2.60")
	const start = "2026-10-20T07:00:00Z"
	detailsURL := e.base() + "/book/details?type=" + e.typ.ID.String() + "&start=" + url.QueryEscape(start)

	rec := c.do("GET", detailsURL, nil)
	mustContain(t, rec, 200, "À prévoir", "Apporter l&#39;ordonnance", "whitespace-pre-line", `name="instructions_ack"`, "J'ai lu les consignes")
	if strings.Contains(rec.Body.String(), "<script>alert(1)") {
		t.Error("consignes non échappées")
	}

	// Sans la case : refus côté serveur, aucun code envoyé.
	form := detailsForm(e.typ.ID.String(), start)
	rec = c.do("POST", detailsURL, form)
	mustContain(t, rec, 422, "Confirmez avoir lu les consignes")
	if e.otp.count() != 0 {
		t.Error("code envoyé sans confirmation des consignes")
	}

	// Avec la case : code envoyé, la case voyage dans les champs cachés.
	form.Set("instructions_ack", "on")
	rec = c.do("POST", detailsURL, form)
	mustContain(t, rec, 200, `name="code"`, `name="instructions_ack" value="on"`)
	m := challengeRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatal("challenge_id absent")
	}
	code := e.otp.code("+33611111111")

	// Confirmation sans la case (champ retiré) : refusée, le code reste valable.
	confirm := detailsForm(e.typ.ID.String(), start)
	confirm.Set("challenge_id", m[1])
	confirm.Set("code", code)
	mustContain(t, c.do("POST", e.base()+"/book/confirm", confirm), 422, "Confirmez avoir lu les consignes")
	if e.appointments(t) != 0 {
		t.Fatal("rendez-vous créé sans confirmation des consignes")
	}
	confirm.Set("instructions_ack", "on")
	rec = c.do("POST", e.base()+"/book/confirm", confirm)
	if rec.Code != 303 {
		t.Fatalf("confirmation: %d\n%s", rec.Code, rec.Body.String())
	}
	manage := rec.Header().Get("Location")

	rec = c.do("GET", manage, nil)
	mustContain(t, rec, 200, "À prévoir", "Apporter l&#39;ordonnance", "whitespace-pre-line")
	if strings.Contains(rec.Body.String(), "<script>alert(1)") {
		t.Error("consignes non échappées sur la confirmation")
	}
	rec = c.do("GET", manage+"/ics", nil)
	mustContain(t, rec, 200, `DESCRIPTION:`, `À prévoir :\nApporter l'ordonnance\nTenue souple`)
}

// Sans case exigée, ou sans consignes, aucune case n'est demandée ni vérifiée.
func TestPublicBookingInstructionsNoAckRequired(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	q := sqlcgen.New(e.db)
	c := newClient(t, e.h, "192.0.2.61")
	const start = "2026-10-20T07:00:00Z"
	detailsURL := e.base() + "/book/details?type=" + e.typ.ID.String() + "&start=" + url.QueryEscape(start)

	// Case exigée mais consignes vides : sans objet.
	if err := q.SetAppointmentTypeInstructions(context.Background(), sqlcgen.SetAppointmentTypeInstructionsParams{
		ID: e.typ.ID, PracticeID: e.pc.ID, InstructionsAckRequired: true,
	}); err != nil {
		t.Fatal(err)
	}
	rec := c.do("GET", detailsURL, nil)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), `name="instructions_ack"`) || strings.Contains(rec.Body.String(), "À prévoir") {
		t.Errorf("case ou bloc affichés sans consignes : %d", rec.Code)
	}
	// Consignes sans case exigée : affichées, envoi accepté sans case.
	if err := q.SetAppointmentTypeInstructions(context.Background(), sqlcgen.SetAppointmentTypeInstructionsParams{
		ID: e.typ.ID, PracticeID: e.pc.ID, PatientInstructions: "Serviette",
	}); err != nil {
		t.Fatal(err)
	}
	rec = c.do("GET", detailsURL, nil)
	if !strings.Contains(rec.Body.String(), "Serviette") || strings.Contains(rec.Body.String(), `name="instructions_ack"`) {
		t.Error("consignes absentes ou case affichée à tort")
	}
	mustContain(t, c.do("POST", detailsURL, detailsForm(e.typ.ID.String(), start)), 200, `name="code"`)
}

func TestAdminTypeInstructions(t *testing.T) {
	t.Parallel()
	_, c := newConfigAdmin(t, domain.RoleAdmin)

	rec := c.do("GET", "/admin/appointment-types", nil)
	mustContain(t, rec, 200, `name="patient_instructions"`, `name="instructions_ack_required"`, "Aucune donnée médicale")

	long := url.Values{"name": {"Drainage"}, "duration": {"50"}, "patient_instructions": {strings.Repeat("é", 1001)}}
	rec = c.do("POST", "/admin/appointment-types", long)
	mustContain(t, rec, 422, "Les consignes sont trop longues")

	ok := url.Values{"name": {"Drainage"}, "duration": {"50"}, "patient_instructions": {"  Apporter une serviette  "}, "instructions_ack_required": {"1"}}
	rec = c.do("POST", "/admin/appointment-types", ok)
	if rec.Code != 303 {
		t.Fatalf("création: %d\n%s", rec.Code, rec.Body.String())
	}
	rec = c.do("GET", "/admin/appointment-types", nil)
	id := regexpTypeID(rec.Body.String(), "Drainage")
	if id == "" {
		t.Fatal("motif créé introuvable")
	}
	rec = c.do("GET", "/admin/appointment-types/"+id, nil)
	mustContain(t, rec, 200, ">Apporter une serviette</textarea>", `name="instructions_ack_required" value="1" checked`)
}

var typeLinkRe = regexp.MustCompile(`/admin/appointment-types/([0-9a-f-]{36})"`)

// regexpTypeID retourne l'id du motif nommé name (premier lien de modification après son nom).
func regexpTypeID(body, name string) string {
	i := strings.Index(body, ">"+name)
	if i < 0 {
		return ""
	}
	if m := typeLinkRe.FindStringSubmatch(body[i:]); m != nil {
		return m[1]
	}
	return ""
}
