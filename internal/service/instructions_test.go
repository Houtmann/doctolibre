package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"doctolibre/internal/domain"
	"doctolibre/internal/notify"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
)

func setInstructions(t *testing.T, e *bookingEnv, text string, ack bool) {
	t.Helper()
	if err := sqlcgen.New(e.db).SetAppointmentTypeInstructions(context.Background(), sqlcgen.SetAppointmentTypeInstructionsParams{
		ID: e.typ.ID, PracticeID: e.pc.ID, PatientInstructions: text, InstructionsAckRequired: ack,
	}); err != nil {
		t.Fatal(err)
	}
}

// T070 : la case « J'ai lu » est exigée par le service (démarrage de la vérification et réservation).
func TestInstructionsAckEnforced(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	setInstructions(t, e, "Apporter l'ordonnance", true)
	ctx := context.Background()
	p := patient("Léa", "Durand", "06 11 11 11 11")

	vin := e.verifyInput(tuesday9, p)
	_, err := e.svc.StartVerification(ctx, vin)
	var ve domain.ErrValidation
	if !errors.As(err, &ve) || ve.Field != "instructions_ack" {
		t.Fatalf("StartVerification sans case : %v", err)
	}
	vin.InstructionsAck = true
	v, err := e.svc.StartVerification(ctx, vin)
	if err != nil {
		t.Fatal(err)
	}
	// Réservation sans la case : refusée, le code n'est pas consommé.
	noAck := vin
	noAck.InstructionsAck = false
	in := service.BookInput{VerifyInput: noAck, ChallengeID: v.ChallengeID, Code: e.otp.code("+33611111111")}
	if _, err := e.svc.Book(ctx, in); !errors.As(err, &ve) || ve.Field != "instructions_ack" {
		t.Fatalf("Book sans case : %v", err)
	}
	in.VerifyInput = vin
	if _, err := e.svc.Book(ctx, in); err != nil {
		t.Fatalf("Book avec case : %v", err)
	}
}

// Les consignes sont lues à la rédaction du message : email avec bloc, SMS inchangé.
func TestInstructionsInMessages(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	e.svc.BaseURL = baseURL
	setInstructions(t, e, "Tenue souple", false)
	ctx := context.Background()
	p := patient("Léa", "Durand", "06 11 11 11 11")
	p.Email = "lea@example.org"
	p.SMSOptIn, p.EmailOptIn = true, true

	vin := e.verifyInput(tuesday9, p)
	vin.Channel = "email"
	v, err := e.svc.StartVerification(ctx, vin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Book(ctx, service.BookInput{VerifyInput: vin, ChallengeID: v.ChallengeID, Code: e.otp.code("lea@example.org")}); err != nil {
		t.Fatal(err)
	}
	ms := outMsgs(t, e.db, e.pc.ID)
	if len(ms) != 1 || ms[0].Kind != "email" {
		t.Fatalf("messages = %+v", ms)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(ms[0].Payload), &data); err != nil {
		t.Fatal(err)
	}
	tpl, err := notify.NewTemplates()
	if err != nil {
		t.Fatal(err)
	}
	em, err := tpl.Render("confirmation", data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(em.Text, "À prévoir :\nTenue souple") {
		t.Errorf("consignes absentes de l'email : %q", em.Text)
	}
	// SMS : la valeur est dans les données mais jamais dans le texte rendu.
	body, err := notify.RenderSMS("confirmation", data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "Tenue souple") {
		t.Errorf("consignes dans le SMS : %q", body)
	}
}
