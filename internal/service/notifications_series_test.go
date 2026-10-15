package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"doctolibre/internal/service"
	"doctolibre/internal/testutil"
)

func TestCarePlanSendsOneConfirmationAndRemindersPerSession(t *testing.T) {
	t.Parallel()
	f := newPlanFixture(t)
	ctx := context.Background()
	prop, err := f.plans.Propose(ctx, f.act, f.input())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.plans.Create(ctx, f.act, service.CreateInput{Plan: f.input(), Lines: f.lines(t, prop)}); err != nil {
		t.Fatal(err)
	}
	ms := outMsgs(t, f.db, f.act.PracticeID)
	if len(ms) != 1 || ms[0].Template != "series" {
		t.Fatalf("%d message(s) %+v, attendu 1 confirmation de série", len(ms), ms)
	}
	if !strings.Contains(ms[0].Payload, "10 séances planifiées") {
		t.Errorf("payload sans le nombre de séances : %s", ms[0].Payload)
	}

	// Les rappels restent par séance (séances : lun 19, jeu 22, lun 26 octobre… à 8 h).
	clock := f.cal.Clock.(*testutil.FixedClock)
	mustExec(t, f.db, "UPDATE appointment SET updated_at = $1", clock.Now())
	rem := &service.Reminders{Pool: f.db, Clock: clock}
	clock.Advance(time.Date(2026, 10, 18, 18, 5, 0, 0, paris).Sub(clock.Now()))
	if n, err := rem.RunOnce(ctx); err != nil || n != 1 {
		t.Fatalf("séance 1 : n=%d err=%v", n, err)
	}
	clock.Advance(time.Date(2026, 10, 21, 18, 5, 0, 0, paris).Sub(clock.Now()))
	if n, err := rem.RunOnce(ctx); err != nil || n != 1 {
		t.Fatalf("séance 2 : n=%d err=%v", n, err)
	}
	if got := len(only(outMsgs(t, f.db, f.act.PracticeID), "reminder")); got != 2 {
		t.Errorf("%d rappels, attendu 2 (un par séance)", got)
	}
}
