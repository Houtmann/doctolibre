package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/testutil"
)

// selfFixture : série de 10 séances (mardi/jeudi, 2 par semaine) dont une seule posée par le
// praticien (mardi 20/10/2026 à 8 h), ouverte à la planification par le patient.
type selfFixture struct {
	planFixture
	planID  uuid.UUID
	token   string
	public  *service.Booking
	created atomic.Int32
	loc     *time.Location
}

func newSelfFixture(t *testing.T, mutate func(f planFixture)) *selfFixture {
	t.Helper()
	f := newPlanFixture(t)
	f.plans.BaseURL = "https://rdv.test"
	mustExec(t, f.db, "UPDATE patient SET email = 'paul@example.test' WHERE id = $1", f.patient.ID)
	mustExec(t, f.db, "UPDATE practice SET care_plan_self_scheduling = true WHERE id = $1", f.act.PracticeID)
	if mutate != nil {
		mutate(f)
	}
	in := f.input()
	in.Weekdays = []string{"1", "3"}
	in.PatientSelfSchedule = true
	plan, err := f.plans.Create(context.Background(), f.act, service.CreateInput{Plan: in, Lines: []service.LineInput{{Date: "2026-10-20", Time: "08:00"}}})
	if err != nil {
		t.Fatalf("création de la série: %v", err)
	}
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	s := &selfFixture{planFixture: f, planID: plan.ID, loc: loc}
	key := service.NewManageKey(testutil.ManageSecret)
	s.token = key.PlanToken(plan.ID, 1)
	s.public = &service.Booking{
		Pool: f.db, Clock: f.cal.Clock, ManageKey: key, BaseURL: "https://rdv.test", Events: f.sink,
		OnCreated: func(string) { s.created.Add(1) },
	}
	return s
}

func (s *selfFixture) sessions(t *testing.T) int {
	t.Helper()
	return s.count(t, "appointment WHERE care_plan_id IS NOT NULL AND status <> 'cancelled'")
}

func (s *selfFixture) propose(t *testing.T, count string, weekdays []string, ws, we string) service.SelfProposal {
	t.Helper()
	p, err := s.public.ProposeSelf(context.Background(), s.token, service.SelfInput{Count: count, Weekdays: weekdays, WindowStart: ws, WindowEnd: we})
	if err != nil {
		t.Fatalf("ProposeSelf: %v", err)
	}
	return p
}

func TestSelfScheduleRemainingSessionsInOneValidation(t *testing.T) {
	t.Parallel()
	// Lundi 19/10/2026 : la 1re semaine proposée a encore son jeudi.
	s := newSelfFixture(t, func(f planFixture) { f.cal.Clock.(*testutil.FixedClock).Advance(4 * 24 * time.Hour) })
	ctx := context.Background()

	view, err := s.public.SelfSchedule(ctx, s.token)
	if err != nil || view.Remaining != 9 || strings.Join(view.Weekdays, ",") != "1,3" {
		t.Fatalf("SelfSchedule = %+v, %v", view, err)
	}
	link, err := s.public.CarePlanByToken(ctx, s.token)
	if err != nil || !link.CanSelfSchedule || link.Remaining != 9 {
		t.Fatalf("CarePlanByToken : CanSelfSchedule=%v Remaining=%d (%v)", link.CanSelfSchedule, link.Remaining, err)
	}

	prop := s.propose(t, "9", view.Weekdays, "", "")
	if len(prop.Lines) != 9 {
		t.Fatalf("%d séances proposées, attendu 9", len(prop.Lines))
	}
	for _, l := range prop.Lines {
		if wd := l.Weekday(); wd != time.Tuesday && wd != time.Thursday {
			t.Errorf("séance un %s : %s", wd, l)
		}
	}
	n, err := s.public.ConfirmSelf(ctx, s.token, service.SelfConfirm{Starts: prop.Lines})
	if err != nil || n != 9 {
		t.Fatalf("ConfirmSelf = %d, %v", n, err)
	}
	if got := s.sessions(t); got != 10 {
		t.Fatalf("%d séances dans la série, attendu 10", got)
	}
	if got := s.count(t, "appointment WHERE care_plan_id IS NOT NULL AND source = 'online'"); got != 9 {
		t.Errorf("%d séances « online », attendu 9", got)
	}
	if got := s.created.Load(); got != 9 {
		t.Errorf("métrique OnCreated appelée %d fois, attendu 9", got)
	}
	// Un seul message récapitulatif pour les 9 séances (la confirmation de la séance posée en plus).
	var series, confirmations, other int
	for _, m := range outMsgs(t, s.db, s.act.PracticeID) {
		switch m.Template {
		case "series":
			series++
			if !strings.Contains(m.Payload, "/s/"+s.token) {
				t.Errorf("le récapitulatif ne porte pas le lien de série : %s", m.Payload)
			}
		case "confirmation":
			confirmations++
		default:
			other++
		}
	}
	if series != 1 || confirmations != 0 || other != 0 {
		t.Errorf("messages : %d récapitulatif, %d confirmation unitaire, %d autres (attendu 1, 0, 0)", series, confirmations, other)
	}
	// À la création de la série (1 séance posée) : une invitation avec le lien, à la place de la confirmation.
	inv := invites(t, s.db, s.act.PracticeID)
	if len(inv) != 1 || !strings.Contains(inv[0], "/s/"+s.token) || !strings.Contains(inv[0], `"remaining": 9`) {
		t.Errorf("invitations : %v", inv)
	}
	// Audit : l'id de la série, jamais de nom.
	var actor string
	var meta string
	if err := s.db.QueryRow(ctx, `SELECT actor, meta::text FROM audit_log WHERE action = 'care_plan.self_scheduled' AND entity_id = $1`, s.planID).Scan(&actor, &meta); err != nil {
		t.Fatalf("audit : %v", err)
	}
	if actor != "patient" || strings.Contains(meta, "Paul") || strings.Contains(meta, "Martin") {
		t.Errorf("audit actor=%q meta=%s", actor, meta)
	}
	// Quota atteint : plus rien à planifier, refus neutre.
	if _, err := s.public.SelfSchedule(ctx, s.token); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("série complète : %v", err)
	}
	if _, err := s.public.ConfirmSelf(ctx, s.token, service.SelfConfirm{Starts: prop.Lines[:1]}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("confirmation d'une série complète : %v", err)
	}
	if got := s.sessions(t); got != 10 {
		t.Errorf("%d séances après rejeu, attendu 10", got)
	}
}

func TestSelfScheduleClosedGates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		sql  string
	}{
		{"réglage du cabinet désactivé", "UPDATE practice SET care_plan_self_scheduling = false WHERE id = (SELECT practice_id FROM care_plan LIMIT 1)"},
		{"case de la série décochée", "UPDATE care_plan SET patient_self_schedule = false"},
		{"série terminée", "UPDATE care_plan SET status = 'completed'"},
		{"série abandonnée", "UPDATE care_plan SET status = 'cancelled'"},
		{"jeton révoqué", "UPDATE care_plan SET token_version = token_version + 1, token_hash = NULL"},
		{"praticien désactivé", "UPDATE practitioner SET active = false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newSelfFixture(t, nil)
			ctx := context.Background()
			// Une proposition valable, prise avant la fermeture.
			lines := s.propose(t, "2", nil, "", "").Lines
			mustExec(t, s.db, tc.sql)
			if _, err := s.public.SelfSchedule(ctx, s.token); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("SelfSchedule : %v", err)
			}
			if _, err := s.public.ProposeSelf(ctx, s.token, service.SelfInput{Count: "2"}); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("ProposeSelf : %v", err)
			}
			if _, err := s.public.ConfirmSelf(ctx, s.token, service.SelfConfirm{Starts: lines}); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("ConfirmSelf : %v", err)
			}
			if got := s.sessions(t); got != 1 {
				t.Errorf("%d séances, attendu 1 (rien de créé)", got)
			}
			if link, err := s.public.CarePlanByToken(ctx, s.token); err == nil && link.CanSelfSchedule {
				t.Error("la page de série propose la planification")
			}
		})
	}
}

func TestSelfScheduleConcurrentConfirmationsNeverExceedPrescribed(t *testing.T) {
	t.Parallel()
	s := newSelfFixture(t, nil)
	ctx := context.Background()
	// Trois « onglets » : chacun veut 5 séances sur des horaires différents (aucun chevauchement
	// entre eux), soit 15 pour 9 restantes. Le verrou de la série n'en laisse passer qu'un.
	windows := [][2]string{{"08:00", "09:00"}, {"09:00", "10:00"}, {"10:00", "11:00"}}
	proposals := make([][]time.Time, len(windows))
	for i, w := range windows {
		proposals[i] = s.propose(t, "5", []string{"1", "3"}, w[0], w[1]).Lines
		if len(proposals[i]) != 5 {
			t.Fatalf("onglet %d : %d séances proposées", i, len(proposals[i]))
		}
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	var ok atomic.Int32
	errs := make([]error, len(windows))
	for i := range windows {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := s.public.ConfirmSelf(ctx, s.token, service.SelfConfirm{Starts: proposals[i]}); err == nil {
				ok.Add(1)
			} else {
				errs[i] = err
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := ok.Load(); got != 1 {
		t.Errorf("%d confirmations acceptées, attendu 1 (erreurs : %v)", got, errs)
	}
	if got := s.sessions(t); got > 10 || got != 6 {
		t.Errorf("%d séances dans la série, attendu 6 (jamais plus de 10 prescrites)", got)
	}
	for _, err := range errs {
		var ve domain.ErrValidation
		if err != nil && !errors.As(err, &ve) {
			t.Errorf("refus inattendu : %v", err)
		}
	}
}

func TestSelfScheduleSameLinesConcurrentlyCreatesOnce(t *testing.T) {
	t.Parallel()
	s := newSelfFixture(t, nil)
	ctx := context.Background()
	lines := s.propose(t, "9", nil, "", "").Lines
	var wg sync.WaitGroup
	var ok atomic.Int32
	for range 4 { // double clic, deux onglets…
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.public.ConfirmSelf(ctx, s.token, service.SelfConfirm{Starts: lines}); err == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || s.sessions(t) != 10 {
		t.Errorf("%d confirmations acceptées, %d séances (attendu 1 et 10)", ok.Load(), s.sessions(t))
	}
}

func TestSelfScheduleConflictCreatesNothingAndProposesAgain(t *testing.T) {
	t.Parallel()
	s := newSelfFixture(t, nil)
	ctx := context.Background()
	lines := s.propose(t, "3", nil, "", "").Lines
	// Quelqu'un prend le 2e créneau entre la proposition et la confirmation.
	taken := lines[1].In(s.loc)
	if _, err := s.cal.Create(ctx, s.act, s.quick(taken.Format("2006-01-02"), taken.Format("15:04"), false)); err != nil {
		t.Fatal(err)
	}
	before := s.sessions(t)
	_, err := s.public.ConfirmSelf(ctx, s.token, service.SelfConfirm{Starts: lines})
	var conflict *service.LineConflict
	if !errors.Is(err, domain.ErrSlotTaken) || !errors.As(err, &conflict) || conflict.Row != 2 {
		t.Fatalf("ConfirmSelf = %v, attendu un conflit à la ligne 2", err)
	}
	if got := s.sessions(t); got != before {
		t.Errorf("%d séances après conflit, attendu %d : tout ou rien", got, before)
	}
	// Une nouvelle proposition évite le créneau pris.
	again := s.propose(t, "3", nil, "", "").Lines
	if len(again) != 3 {
		t.Fatalf("nouvelle proposition : %d séances", len(again))
	}
	for _, l := range again {
		if l.Equal(lines[1]) {
			t.Error("le créneau pris est reproposé")
		}
	}
	if n, err := s.public.ConfirmSelf(ctx, s.token, service.SelfConfirm{Starts: again}); err != nil || n != 3 {
		t.Errorf("ConfirmSelf de la nouvelle proposition = %d, %v", n, err)
	}
}

func TestSelfScheduleRespectsNoticeHorizonAndHolidays(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

	t.Run("préavis", func(t *testing.T) {
		t.Parallel()
		s := newSelfFixture(t, func(f planFixture) {
			mustExec(t, f.db, "UPDATE practice SET min_booking_notice = '30 days' WHERE id = $1", f.act.PracticeID)
		})
		// Le préavis ne s'applique pas à la séance posée par le cabinet (staffSlotContext) ; la proposition
		// du patient commence 30 jours plus tard.
		for _, l := range s.propose(t, "4", nil, "", "").Lines {
			if l.Before(now.AddDate(0, 0, 30)) {
				t.Errorf("séance proposée %s avant la fin du préavis de 30 jours", l)
			}
		}
		if _, err := s.public.ConfirmSelf(context.Background(), s.token, service.SelfConfirm{Starts: []time.Time{time.Date(2026, 10, 22, 6, 0, 0, 0, time.UTC)}}); !errors.Is(err, domain.ErrSlotTaken) {
			t.Errorf("confirmation dans le préavis : %v", err)
		}
	})
	t.Run("horizon", func(t *testing.T) {
		t.Parallel()
		s := newSelfFixture(t, func(f planFixture) {
			mustExec(t, f.db, "UPDATE practice SET booking_horizon = '12 days' WHERE id = $1", f.act.PracticeID)
		})
		p := s.propose(t, "9", nil, "", "")
		if len(p.Lines) == 0 || len(p.Lines) >= 9 {
			t.Errorf("%d séances proposées dans un horizon de 12 jours", len(p.Lines))
		}
		for _, l := range p.Lines {
			if l.After(now.AddDate(0, 0, 12)) {
				t.Errorf("séance %s au-delà de l'horizon", l)
			}
		}
		if _, err := s.public.ConfirmSelf(context.Background(), s.token, service.SelfConfirm{Starts: []time.Time{time.Date(2026, 12, 1, 7, 0, 0, 0, time.UTC)}}); !errors.Is(err, domain.ErrSlotTaken) {
			t.Errorf("confirmation hors horizon : %v", err)
		}
	})
	t.Run("jours fériés", func(t *testing.T) {
		t.Parallel()
		s := newSelfFixture(t, func(f planFixture) {
			mustExec(t, f.db, "UPDATE practice SET close_on_holidays = true WHERE id = $1", f.act.PracticeID)
		})
		// Dimanche 1er novembre (Toussaint) et mercredi 11 novembre : fermés.
		p := s.propose(t, "9", []string{"6"}, "", "")
		if len(p.Lines) == 0 {
			t.Fatal("aucune séance proposée")
		}
		for _, l := range p.Lines {
			if d := l.In(s.loc).Format("2006-01-02"); d == "2026-11-01" || d == "2026-11-11" {
				t.Errorf("séance proposée un jour férié : %s", d)
			}
		}
		if _, err := s.public.ConfirmSelf(context.Background(), s.token, service.SelfConfirm{Starts: []time.Time{time.Date(2026, 11, 1, 7, 0, 0, 0, time.UTC)}}); !errors.Is(err, domain.ErrSlotTaken) {
			t.Errorf("confirmation un jour férié : %v", err)
		}
	})
}

func TestSelfScheduleAcrossDaylightSavingChange(t *testing.T) {
	t.Parallel()
	s := newSelfFixture(t, nil)
	// Dimanche 25/10/2026 : passage à l'heure d'hiver (3 h → 2 h) ; 8 h reste 8 h au mur le jour même.
	p := s.propose(t, "5", []string{"6"}, "08:00", "09:00")
	byDay := map[string]time.Time{}
	for _, l := range p.Lines {
		local := l.In(s.loc)
		if local.Hour() != 8 || local.Minute() != 0 {
			t.Errorf("séance à %s : l'heure murale doit rester 08:00", local)
		}
		byDay[local.Format("2006-01-02")] = l
	}
	// 8 h au mur = 6 h UTC en heure d'été (jusqu'au 24/10), 7 h UTC ensuite.
	for day, wantUTC := range map[string]int{"2026-10-18": 6, "2026-10-25": 7, "2026-11-01": 7} {
		l, ok := byDay[day]
		if !ok {
			t.Errorf("pas de séance le %s (proposées : %v)", day, p.Lines)
			continue
		}
		if l.UTC().Hour() != wantUTC {
			t.Errorf("%s : %s UTC, attendu %d h", day, l.UTC(), wantUTC)
		}
	}
	if n, err := s.public.ConfirmSelf(context.Background(), s.token, service.SelfConfirm{Starts: p.Lines}); err != nil || n != len(p.Lines) {
		t.Fatalf("ConfirmSelf = %d, %v", n, err)
	}
}

func TestSelfScheduleNextSlotAndPreferences(t *testing.T) {
	t.Parallel()
	s := newSelfFixture(t, nil)
	in := service.SelfInput{Count: "3", Weekdays: []string{"1", "3"}}
	p := s.propose(t, "3", in.Weekdays, "", "")
	got, found, err := s.public.NextSelf(context.Background(), s.token, in, p.Lines, 1)
	if err != nil || !found || len(got.Lines) != 3 {
		t.Fatalf("NextSelf = %+v, %v, %v", got, found, err)
	}
	if !got.Lines[0].Equal(p.Lines[0]) {
		t.Errorf("la 1re séance a changé : %s -> %s", p.Lines[0], got.Lines[0])
	}
	moved := false
	for _, l := range got.Lines {
		if l.Equal(p.Lines[1]) {
			t.Error("la séance demandée « suivante » est restée en place")
		}
		if l.After(p.Lines[1]) {
			moved = true
		}
	}
	if !moved {
		t.Error("aucune séance après l'ancienne")
	}
	// Plus de nombre que de séances restantes : refus de saisie.
	if _, err := s.public.ProposeSelf(context.Background(), s.token, service.SelfInput{Count: "10"}); err == nil {
		t.Error("10 séances demandées pour 9 restantes")
	}
	if _, err := s.public.ConfirmSelf(context.Background(), s.token, service.SelfConfirm{Starts: append(p.Lines, p.Lines...)}); err == nil {
		t.Error("deux fois le même horaire accepté")
	}
}

func TestSelfScheduleNoShowGuardApplies(t *testing.T) {
	t.Parallel()
	s := newSelfFixture(t, nil)
	ctx := context.Background()
	lines := s.propose(t, "2", nil, "", "").Lines
	mustExec(t, s.db, "UPDATE practice SET no_show_block_threshold = 1 WHERE id = $1", s.act.PracticeID)
	mustExec(t, s.db, `UPDATE appointment SET status = 'no_show' WHERE patient_id = $1`, s.patient.ID)
	if _, err := s.public.ProposeSelf(ctx, s.token, service.SelfInput{Count: "2"}); !errors.Is(err, domain.ErrOnlineBookingBlocked) {
		t.Errorf("ProposeSelf : %v", err)
	}
	if _, err := s.public.ConfirmSelf(ctx, s.token, service.SelfConfirm{Starts: lines}); !errors.Is(err, domain.ErrOnlineBookingBlocked) {
		t.Errorf("ConfirmSelf : %v", err)
	}
	if got := s.count(t, "appointment WHERE status = 'booked'"); got != 0 {
		t.Errorf("%d séances créées malgré le blocage", got)
	}
}

func TestSelfScheduleRequiresInstructionsAck(t *testing.T) {
	t.Parallel()
	s := newSelfFixture(t, nil)
	ctx := context.Background()
	mustExec(t, s.db, "UPDATE appointment_type SET patient_instructions = 'Venir en tenue souple.', instructions_ack_required = true WHERE id = $1", s.typeID)
	view, err := s.public.SelfSchedule(ctx, s.token)
	if err != nil || !view.AckRequired || view.Instructions != "Venir en tenue souple." {
		t.Fatalf("SelfSchedule = %+v, %v", view, err)
	}
	lines := s.propose(t, "2", nil, "", "").Lines
	if _, err := s.public.ConfirmSelf(ctx, s.token, service.SelfConfirm{Starts: lines}); err == nil || !strings.Contains(err.Error(), "consignes") {
		t.Fatalf("sans accusé : %v", err)
	}
	if got := s.sessions(t); got != 1 {
		t.Fatalf("%d séances sans accusé, attendu 1", got)
	}
	if n, err := s.public.ConfirmSelf(ctx, s.token, service.SelfConfirm{Starts: lines, InstructionsAck: true}); err != nil || n != 2 {
		t.Errorf("avec accusé : %d, %v", n, err)
	}
}

func TestSelfScheduleHomeVisitsReuseSeriesAddress(t *testing.T) {
	t.Parallel()
	f := newPlanFixture(t)
	ctx := context.Background()
	mustExec(t, f.db, "UPDATE appointment_type SET at_home = true, travel_buffer = '15 minutes' WHERE id = $1", f.typeID)
	mustExec(t, f.db, "UPDATE practice SET care_plan_self_scheduling = true WHERE id = $1", f.act.PracticeID)
	in := f.input()
	in.Weekdays, in.PatientSelfSchedule, in.Address = []string{"1", "3"}, true, "12 rue des Lilas, 75011 Paris"
	// À domicile, l'adresse vient des séances posées : au moins une.
	if _, err := f.plans.Create(ctx, f.act, service.CreateInput{Plan: in}); err == nil {
		t.Fatal("série à domicile sans séance acceptée")
	}
	plan, err := f.plans.Create(ctx, f.act, service.CreateInput{Plan: in, Lines: []service.LineInput{{Date: "2026-10-20", Time: "08:00"}}})
	if err != nil {
		t.Fatal(err)
	}
	key := service.NewManageKey(testutil.ManageSecret)
	pub := &service.Booking{Pool: f.db, Clock: f.cal.Clock, ManageKey: key}
	tok := key.PlanToken(plan.ID, 1)
	p, err := pub.ProposeSelf(ctx, tok, service.SelfInput{Count: "2", Weekdays: []string{"1", "3"}})
	if err != nil || len(p.Lines) != 2 {
		t.Fatalf("ProposeSelf = %d lignes, %v", len(p.Lines), err)
	}
	if _, err := pub.ConfirmSelf(ctx, tok, service.SelfConfirm{Starts: p.Lines}); err != nil {
		t.Fatal(err)
	}
	if got := f.count(t, "appointment WHERE care_plan_id IS NOT NULL AND address = '12 rue des Lilas, 75011 Paris'"); got != 3 {
		t.Errorf("%d séances avec l'adresse de la série, attendu 3", got)
	}
	// Marge de trajet comprise dans la plage bloquée (30 min de soin + 15 min de trajet avant et après + 10 min de marge du motif).
	var blocked time.Duration
	if err := f.db.QueryRow(ctx, "SELECT upper(during_blocked) - lower(during_blocked) FROM appointment WHERE care_plan_id = $1 ORDER BY lower(during) DESC LIMIT 1", plan.ID).Scan(&blocked); err != nil {
		t.Fatal(err)
	}
	if blocked <= 30*time.Minute {
		t.Errorf("plage bloquée %s : le trajet n'est pas compté", blocked)
	}
	// Sans aucune adresse connue, la planification en ligne est refusée.
	mustExec(t, f.db, "UPDATE appointment SET address = NULL")
	if _, err := pub.SelfSchedule(ctx, tok); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("sans adresse : %v", err)
	}
	if link, err := pub.CarePlanByToken(ctx, tok); err != nil || link.CanSelfSchedule {
		t.Errorf("la page propose la planification sans adresse (%v)", err)
	}
}

func TestCreateSeriesWithoutSessionsNeedsSelfScheduling(t *testing.T) {
	t.Parallel()
	f := newPlanFixture(t)
	ctx := context.Background()
	in := f.input()
	if _, err := f.plans.Create(ctx, f.act, service.CreateInput{Plan: in}); err == nil {
		t.Fatal("série sans séance acceptée sans auto-planification")
	}
	in.PatientSelfSchedule = true
	if _, err := f.plans.Create(ctx, f.act, service.CreateInput{Plan: in}); err == nil {
		t.Fatal("auto-planification acceptée alors que le réglage du cabinet est désactivé")
	}
	mustExec(t, f.db, "UPDATE practice SET care_plan_self_scheduling = true WHERE id = $1", f.act.PracticeID)
	plan, err := f.plans.Create(ctx, f.act, service.CreateInput{Plan: in})
	if err != nil {
		t.Fatalf("série sans séance : %v", err)
	}
	if !plan.PatientSelfSchedule || f.count(t, "appointment") != 0 {
		t.Errorf("série %+v, %d rendez-vous", plan, f.count(t, "appointment"))
	}
	if msgs := outMsgs(t, f.db, f.act.PracticeID); len(msgs) != 0 {
		t.Errorf("%d messages pour une série sans séance", len(msgs))
	}
	if u := f.plans.LinkURL(plan); !strings.HasPrefix(u, "/s/") {
		t.Errorf("LinkURL = %q", u)
	}
	// Le patient planifie alors toute la série.
	key := service.NewManageKey(testutil.ManageSecret)
	pub := &service.Booking{Pool: f.db, Clock: f.cal.Clock, ManageKey: key}
	tok := key.PlanToken(plan.ID, 1)
	p, err := pub.ProposeSelf(ctx, tok, service.SelfInput{Count: "10", Weekdays: []string{"1", "3"}})
	if err != nil || len(p.Lines) != 10 {
		t.Fatalf("ProposeSelf = %d lignes, %v", len(p.Lines), err)
	}
	if n, err := pub.ConfirmSelf(ctx, tok, service.SelfConfirm{Starts: p.Lines}); err != nil || n != 10 {
		t.Fatalf("ConfirmSelf = %d, %v", n, err)
	}
}

// invites retourne les payloads des invitations de série (messages sans rendez-vous rattaché).
func invites(t *testing.T, db *pgxpool.Pool, practiceID uuid.UUID) []string {
	t.Helper()
	rows, err := db.Query(context.Background(),
		`SELECT payload::text FROM outbox_message WHERE practice_id = $1 AND template = 'series_invite' AND appointment_id IS NULL ORDER BY created_at, id`, practiceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func TestSelfScheduleInviteSentWithoutAnySession(t *testing.T) {
	t.Parallel()
	f := newPlanFixture(t)
	f.plans.BaseURL = "https://rdv.test"
	ctx := context.Background()
	mustExec(t, f.db, "UPDATE patient SET email = 'paul@example.test' WHERE id = $1", f.patient.ID)
	mustExec(t, f.db, "UPDATE practice SET care_plan_self_scheduling = true WHERE id = $1", f.act.PracticeID)
	in := f.input()
	in.PatientSelfSchedule = true
	plan, err := f.plans.Create(ctx, f.act, service.CreateInput{Plan: in})
	if err != nil {
		t.Fatal(err)
	}
	tok := service.NewManageKey(testutil.ManageSecret).PlanToken(plan.ID, 1)
	inv := invites(t, f.db, f.act.PracticeID)
	if len(inv) != 1 || !strings.Contains(inv[0], "https://rdv.test/s/"+tok) || !strings.Contains(inv[0], `"remaining": 10`) {
		t.Fatalf("invitations : %v", inv)
	}
	if len(outMsgs(t, f.db, f.act.PracticeID)) != 0 {
		t.Error("message rattaché à un rendez-vous alors qu'aucun n'existe")
	}
	// Consentements : sans contact utilisable, rien n'est envoyé.
	mustExec(t, f.db, "UPDATE patient SET email_opt_in = false, sms_opt_in = false WHERE id = $1", f.patient.ID)
	if _, err := f.plans.Create(ctx, f.act, service.CreateInput{Plan: in}); err != nil {
		t.Fatal(err)
	}
	if got := len(invites(t, f.db, f.act.PracticeID)); got != 1 {
		t.Errorf("%d invitations après refus des notifications, attendu 1", got)
	}
}

func TestSelfScheduleConfirmRevalidatesGapAndWeeklyQuota(t *testing.T) {
	t.Parallel()
	s := newSelfFixture(t, nil) // 2 séances par semaine, 1 posée le mardi 20/10
	ctx := context.Background()
	at := func(day, hh string) time.Time {
		tm, err := time.ParseInLocation("2006-01-02 15:04", day+" "+hh, s.loc)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	cases := []struct {
		name   string
		starts []time.Time
	}{
		{"deux séances le même jour", []time.Time{at("2026-10-27", "08:00"), at("2026-10-27", "09:30")}},
		{"écart minimal avec une séance existante", []time.Time{at("2026-10-21", "08:00")}},
		{"trop de séances dans la semaine", []time.Time{at("2026-10-22", "08:00"), at("2026-10-25", "08:00")}},
	}
	for _, tc := range cases {
		if _, err := s.public.ConfirmSelf(ctx, s.token, service.SelfConfirm{Starts: tc.starts}); err == nil {
			t.Errorf("%s : forgé accepté", tc.name)
		}
	}
	if got := s.sessions(t); got != 1 {
		t.Fatalf("%d séances, attendu 1 : rien ne doit être créé", got)
	}
	// Une saisie conforme passe : jeudi 22 (écart de 2 jours, 2e séance de la semaine).
	if n, err := s.public.ConfirmSelf(ctx, s.token, service.SelfConfirm{Starts: []time.Time{at("2026-10-22", "08:00")}}); err != nil || n != 1 {
		t.Errorf("saisie conforme : %d, %v", n, err)
	}
}
