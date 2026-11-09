package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"doctolibre/internal/notify"
	"doctolibre/internal/notify/sms"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
)

const ackTemplate = service.TemplatePatientCancellation

func TestCancelByPatientQueuesSMSAckOnce(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	// Le patient a un email, mais le canal vérifié est le SMS : pas d'email.
	e.svc.BaseURL = baseURL
	p := patient("Léa", "Durand", "06 11 11 11 11")
	p.Email = "lea@example.org"
	res, err := e.svc.Book(context.Background(), e.start(t, tuesday9, p, "+33611111111"))
	if err != nil {
		t.Fatal(err)
	}

	if err := e.svc.CancelByPatient(context.Background(), res.Token); err != nil {
		t.Fatal(err)
	}
	acks := only(outMsgs(t, e.db, e.pc.ID), ackTemplate)
	if len(acks) != 1 {
		t.Fatalf("%d accusé(s), attendu 1 : %+v", len(acks), acks)
	}
	m := acks[0]
	if m.Kind != "sms" || m.Recipient != "+33611111111" || m.Status != "pending" {
		t.Errorf("accusé = %+v", m)
	}
	if want := ackTemplate + ":" + res.Appointment.ID.String() + ":sms"; m.DedupKey != want {
		t.Errorf("dedup_key = %q, attendu %q", m.DedupKey, want)
	}
	b := m.body(t)
	if !strings.Contains(b, baseURL+"/r/") {
		t.Errorf("le SMS ne contient pas le lien de reprise : %q", b)
	}
	if n, ucs2 := sms.Segments(b); n != 1 || ucs2 {
		t.Errorf("SMS de %d segment(s), UCS-2=%v : %q", n, ucs2, b)
	}
	// Le rappel et la confirmation encore en attente ne partent plus.
	for _, o := range outMsgs(t, e.db, e.pc.ID) {
		if o.Template != ackTemplate && o.Status == "pending" {
			t.Errorf("message en attente après annulation : %+v", o)
		}
	}

	// Annulation rejouée : refusée, rien de plus enfilé.
	if err := e.svc.CancelByPatient(context.Background(), res.Token); !errors.Is(err, service.ErrChangeTooLate) {
		t.Fatalf("seconde annulation = %v, want ErrChangeTooLate", err)
	}
	if n := len(only(outMsgs(t, e.db, e.pc.ID), ackTemplate)); n != 1 {
		t.Errorf("%d accusé(s) après rejeu, attendu 1", n)
	}
}

func TestCancelByPatientQueuesEmailAckWithRebookLink(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	e.svc.BaseURL = baseURL
	p := patient("Léa", "Durand", "06 11 11 11 11")
	p.Email = "lea@example.org"
	vin := e.verifyInput(tuesday9, p)
	vin.Channel = "email"
	v, err := e.svc.StartVerification(context.Background(), vin)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.svc.Book(context.Background(), service.BookInput{VerifyInput: vin, ChallengeID: v.ChallengeID, Code: e.otp.code("lea@example.org")})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.CancelByPatient(context.Background(), res.Token); err != nil {
		t.Fatal(err)
	}
	acks := only(outMsgs(t, e.db, e.pc.ID), ackTemplate)
	if len(acks) != 1 || acks[0].Kind != "email" || acks[0].Recipient != "lea@example.org" {
		t.Fatalf("accusés = %+v, attendu 1 email", acks)
	}
	tpl, err := notify.NewTemplates()
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(acks[0].Payload), &data); err != nil {
		t.Fatal(err)
	}
	em, err := tpl.Render(ackTemplate, data)
	if err != nil {
		t.Fatal(err)
	}
	// Reprise directe sans ressaisie (T098) quand elle est permise.
	want := baseURL + "/r/" + res.Token + "/rebook"
	if !strings.Contains(em.Text, want) || !strings.Contains(em.HTML, want) {
		t.Errorf("lien de reprise %q absent de l'email :\n%s", want, em.Text)
	}

	// Motif non réservable en ligne ou désactivé : lien vers la page du praticien.
	for _, off := range []func(*sqlcgen.AppointmentType){
		func(a *sqlcgen.AppointmentType) { a.OnlineBookable = false },
		func(a *sqlcgen.AppointmentType) { a.Active = false },
	} {
		typ := e.typ
		off(&typ)
		if got := service.RebookPath(e.pc.Slug, e.pr, typ); strings.Contains(got, "/book") || !strings.HasSuffix(got, "/"+e.pr.Slug) {
			t.Errorf("RebookPath avec motif non réservable = %q", got)
		}
	}
}

func TestCancelByPatientAckRolledBackWithCancellation(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	e.svc.BaseURL = baseURL
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	mustExec(t, e.db, `
		CREATE FUNCTION fail_ack() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'échec simulé'; END $$;
		CREATE TRIGGER fail_ack BEFORE INSERT ON outbox_message FOR EACH ROW
		WHEN (NEW.template = 'patient_cancellation') EXECUTE FUNCTION fail_ack();`)
	if err := e.svc.CancelByPatient(context.Background(), token); err == nil {
		t.Fatal("annulation réussie malgré l'échec de l'enfilage")
	}
	if st, _, _ := e.status(t, token); st != "booked" {
		t.Errorf("statut = %q, attendu booked (annulation et accusé dans la même transaction)", st)
	}
}
