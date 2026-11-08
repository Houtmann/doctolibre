package service

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// ActionStatsExported : action d'audit de l'export CSV des statistiques (T073).
const ActionStatsExported = "stats.exported"

// MaxStatsDays : durée maximale d'une période de statistiques (bornes incluses).
const MaxStatsDays = 366

const dateLayout = "2006-01-02"

// utf8BOM : marque d'octets UTF-8 en tête du CSV (Excel FR).
const utf8BOM = "\xEF\xBB\xBF"

// Stats calcule les statistiques de séances (faites, absences, annulations) pour la rétrocession.
type Stats struct {
	Pool  *pgxpool.Pool
	Clock domain.Clock
}

// StatsParams est la demande : From/To au format AAAA-MM-JJ (jours du cabinet, bornes
// incluses ; vides tous les deux = mois civil précédent), Practitioner uuid.Nil = tous
// (admin) ou soi-même (praticien).
type StatsParams struct {
	From, To     string
	Practitioner uuid.UUID
}

// StatsCounts regroupe les compteurs d'une ligne ou d'un total.
type StatsCounts struct {
	Done, NoShow, Cancelled int64
	// HomeDone : séances faites à domicile (incluses dans Done).
	HomeDone int64
}

func (c *StatsCounts) add(o StatsCounts) {
	c.Done += o.Done
	c.NoShow += o.NoShow
	c.Cancelled += o.Cancelled
	c.HomeDone += o.HomeDone
}

// StatsLine : un motif pour un réalisateur.
type StatsLine struct {
	TypeID uuid.UUID
	Type   string
	AtHome bool
	StatsCounts
}

// StatsPerformer : les lignes d'un réalisateur (le titulaire ou un remplaçant).
type StatsPerformer struct {
	ID         uuid.UUID
	Name       string
	Substitute bool
	Lines      []StatsLine
	Total      StatsCounts
}

// StatsGroup : les séances d'un praticien titulaire.
type StatsGroup struct {
	ID         uuid.UUID
	Name       string
	Performers []StatsPerformer
	Total      StatsCounts
}

// StatsChoice est un praticien proposé dans le filtre (administrateur seulement).
type StatsChoice struct {
	ID   uuid.UUID
	Name string
}

// StatsReport est le résultat : From/To sont des jours AAAA-MM-JJ inclus.
type StatsReport struct {
	From, To     string
	Practitioner uuid.UUID // filtre effectif (uuid.Nil = tous)
	Groups       []StatsGroup
	Total        StatsCounts
	Choices      []StatsChoice
	Timezone     string
}

// statsScope vérifie l'accès et retourne le praticien visé (uuid.Nil = tous).
// Admin : tous ou un praticien du cabinet. Praticien : lui-même seulement (jamais le
// titulaire dont il a une délégation). Secrétariat et autres : refusé.
func (s *Stats) statsScope(ctx context.Context, a Actor, want uuid.UUID) (uuid.UUID, error) {
	switch a.Role {
	case domain.RoleAdmin:
		if want == uuid.Nil {
			return uuid.Nil, nil
		}
		if _, err := sqlcgen.New(s.Pool).GetPractitioner(ctx, sqlcgen.GetPractitionerParams{ID: want, PracticeID: a.PracticeID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return uuid.Nil, domain.ErrNotFound
			}
			return uuid.Nil, fmt.Errorf("praticien: %w", err)
		}
		return want, nil
	case domain.RolePractitioner:
		if a.PractitionerID == uuid.Nil || (want != uuid.Nil && want != a.PractitionerID) {
			return uuid.Nil, domain.ErrForbidden
		}
		return a.PractitionerID, nil
	}
	return uuid.Nil, domain.ErrForbidden
}

// statsPeriod interprète From/To dans le fuseau du cabinet et retourne les jours
// (minuit local) et la borne de fin exclusive : minuit local du lendemain du dernier jour.
func statsPeriod(p StatsParams, now time.Time, loc *time.Location) (from, to, endExcl time.Time, err error) {
	switch {
	case p.From == "" && p.To == "":
		y, m, _ := now.In(loc).Date()
		from = time.Date(y, m-1, 1, 0, 0, 0, 0, loc)
		to = time.Date(y, m, 0, 0, 0, 0, 0, loc) // dernier jour du mois précédent
	case p.From == "" || p.To == "":
		return from, to, endExcl, domain.ErrValidation{Field: "from", Msg: "Indiquez les deux dates de la période."}
	default:
		if from, err = time.ParseInLocation(dateLayout, p.From, loc); err != nil {
			return from, to, endExcl, domain.ErrValidation{Field: "from", Msg: "Date de début invalide."}
		}
		if to, err = time.ParseInLocation(dateLayout, p.To, loc); err != nil {
			return from, to, endExcl, domain.ErrValidation{Field: "to", Msg: "Date de fin invalide."}
		}
	}
	if to.Before(from) {
		return from, to, endExcl, domain.ErrValidation{Field: "to", Msg: "La date de fin précède la date de début."}
	}
	if to.After(from.AddDate(0, 0, MaxStatsDays-1)) {
		return from, to, endExcl, domain.ErrValidation{Field: "to", Msg: fmt.Sprintf("La période ne peut pas dépasser %d jours.", MaxStatsDays)}
	}
	return from, to, to.AddDate(0, 0, 1), nil
}

func fullName(first, last string) string { return strings.TrimSpace(first + " " + last) }

// Report calcule les statistiques de la période.
func (s *Stats) Report(ctx context.Context, a Actor, p StatsParams) (StatsReport, error) {
	target, err := s.statsScope(ctx, a, p.Practitioner)
	if err != nil {
		return StatsReport{}, err
	}
	q := sqlcgen.New(s.Pool)
	pc, err := q.GetPractice(ctx, a.PracticeID)
	if err != nil {
		return StatsReport{}, fmt.Errorf("cabinet: %w", err)
	}
	loc, err := time.LoadLocation(pc.Timezone)
	if err != nil {
		return StatsReport{}, fmt.Errorf("fuseau du cabinet: %w", err)
	}
	from, to, endExcl, err := statsPeriod(p, s.Clock.Now(), loc)
	if err != nil {
		return StatsReport{}, err
	}
	rows, err := q.StatsSessions(ctx, sqlcgen.StatsSessionsParams{
		PracticeID: a.PracticeID, RangeStart: from, RangeEnd: endExcl,
		PractitionerID: pgtype.UUID{Bytes: target, Valid: target != uuid.Nil},
	})
	if err != nil {
		return StatsReport{}, fmt.Errorf("statistiques: %w", err)
	}
	rep := StatsReport{From: from.Format(dateLayout), To: to.Format(dateLayout), Practitioner: target, Timezone: pc.Timezone}
	for _, r := range rows {
		var c StatsCounts
		switch r.Status {
		case "done":
			c.Done = r.Total
			if r.AtHome {
				c.HomeDone = r.Total
			}
		case "no_show":
			c.NoShow = r.Total
		case "cancelled":
			c.Cancelled = r.Total
		}
		if n := len(rep.Groups); n == 0 || rep.Groups[n-1].ID != r.PractitionerID {
			rep.Groups = append(rep.Groups, StatsGroup{ID: r.PractitionerID, Name: fullName(r.PractitionerFirstName, r.PractitionerLastName)})
		}
		g := &rep.Groups[len(rep.Groups)-1]
		if n := len(g.Performers); n == 0 || g.Performers[n-1].ID != r.PerformedBy {
			g.Performers = append(g.Performers, StatsPerformer{
				ID: r.PerformedBy, Name: fullName(r.PerformerFirstName, r.PerformerLastName), Substitute: r.PerformedBy != r.PractitionerID,
			})
		}
		pf := &g.Performers[len(g.Performers)-1]
		if n := len(pf.Lines); n == 0 || pf.Lines[n-1].TypeID != r.AppointmentTypeID {
			pf.Lines = append(pf.Lines, StatsLine{TypeID: r.AppointmentTypeID, Type: r.TypeName, AtHome: r.AtHome})
		}
		pf.Lines[len(pf.Lines)-1].add(c)
		pf.Total.add(c)
		g.Total.add(c)
		rep.Total.add(c)
	}
	if a.Role == domain.RoleAdmin {
		prs, err := q.ListPractitioners(ctx, sqlcgen.ListPractitionersParams{PracticeID: a.PracticeID, PageSize: pageSize})
		if err != nil {
			return StatsReport{}, fmt.Errorf("liste des praticiens: %w", err)
		}
		for _, pr := range prs {
			rep.Choices = append(rep.Choices, StatsChoice{ID: pr.ID, Name: fullName(pr.FirstName, pr.LastName)})
		}
	}
	return rep, nil
}

// StatsCSVHeader : en-têtes de l'export (aucune donnée patient).
var StatsCSVHeader = []string{"Praticien titulaire", "Réalisé par", "Rôle", "Motif", "Lieu", "Faites", "Absents", "Annulées", "Dont à domicile"}

// statsCSVSafe neutralise l'injection de formules des tableurs : une cellule qui
// commence par = + - @ tabulation ou retour chariot est préfixée par une apostrophe.
func statsCSVSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// ExportCSV calcule les statistiques, journalise l'export (période et praticien,
// jamais de nom) puis écrit le CSV (`;`, UTF-8 avec BOM, CRLF). Retourne le rapport
// pour que l'appelant connaisse les bornes effectives de la période.
func (s *Stats) ExportCSV(ctx context.Context, a Actor, p StatsParams, w io.Writer) (StatsReport, error) {
	rep, err := s.Report(ctx, a, p)
	if err != nil {
		return StatsReport{}, err
	}
	err = store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		return writeAudit(ctx, q, a.PracticeID, uuid.NullUUID{UUID: a.UserID, Valid: true}, ActionStatsExported, "practice", a.PracticeID, map[string]string{
			"from": rep.From, "to": rep.To, "practitioner": rep.Practitioner.String(),
		})
	})
	if err != nil {
		return StatsReport{}, err
	}
	if _, err := io.WriteString(w, utf8BOM); err != nil {
		return rep, err
	}
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	cw.UseCRLF = true
	if err := cw.Write(StatsCSVHeader); err != nil {
		return rep, err
	}
	for _, g := range rep.Groups {
		for _, pf := range g.Performers {
			role := "titulaire"
			if pf.Substitute {
				role = "remplaçant"
			}
			for _, l := range pf.Lines {
				place := "cabinet"
				if l.AtHome {
					place = "domicile"
				}
				rec := []string{
					statsCSVSafe(g.Name), statsCSVSafe(pf.Name), role, statsCSVSafe(l.Type), place,
					fmt.Sprint(l.Done), fmt.Sprint(l.NoShow), fmt.Sprint(l.Cancelled), fmt.Sprint(l.HomeDone),
				}
				if err := cw.Write(rec); err != nil {
					return rep, err
				}
			}
		}
	}
	cw.Flush()
	return rep, cw.Error()
}
