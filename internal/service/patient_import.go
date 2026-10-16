package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// Statuts d'une ligne analysée.
const (
	ImportOK        = "ok"
	ImportInvalid   = "invalid"
	ImportDuplicate = "duplicate"
)

// Bornes de l'aperçu (le fichier entier est toujours analysé).
const (
	importSampleSize  = 10
	importProblemsMax = 200
)

// PatientImport importe une liste de patients depuis un CSV : aperçu puis
// insertion atomique. Le fichier n'est jamais conservé : l'appelant le renvoie
// à la confirmation et il est ré-analysé.
type PatientImport struct {
	Pool  *pgxpool.Pool
	Clock domain.Clock
}

// ImportLine est le résultat de l'analyse d'une ligne du fichier.
type ImportLine struct {
	Line         int
	Name         string // « NOM Prénom »
	Phone, Email string // normalisés (E.164, minuscules)
	Status       string
	Reason       string // vide si Status == ImportOK

	row ImportRow
	d   details
}

// ImportPreview est l'aperçu d'un fichier.
type ImportPreview struct {
	Total, Valid, Invalid, Duplicates int
	Sample                            []ImportLine // premières lignes importables
	Problems                          []ImportLine // lignes invalides ou doublons (bornées)
	MoreProblems                      int          // lignes à problème non listées
}

// MoreValid est le nombre de lignes importables absentes de l'échantillon.
func (p ImportPreview) MoreValid() int { return p.Valid - len(p.Sample) }

// ImportResult est le bilan d'un import.
type ImportResult struct {
	Imported, Skipped int
}

// Preview analyse le fichier sans rien écrire.
func (s *PatientImport) Preview(ctx context.Context, a Actor, data []byte) (ImportPreview, error) {
	rows, err := ParseImportCSV(data)
	if err != nil {
		return ImportPreview{}, err
	}
	lines, err := s.analyze(ctx, sqlcgen.New(s.Pool), a, rows)
	if err != nil {
		return ImportPreview{}, err
	}
	p := ImportPreview{Total: len(lines)}
	for _, l := range lines {
		switch l.Status {
		case ImportOK:
			p.Valid++
			if len(p.Sample) < importSampleSize {
				p.Sample = append(p.Sample, l)
			}
			continue
		case ImportInvalid:
			p.Invalid++
		case ImportDuplicate:
			p.Duplicates++
		}
		if len(p.Problems) < importProblemsMax {
			p.Problems = append(p.Problems, l)
		} else {
			p.MoreProblems++
		}
	}
	return p, nil
}

// importJSON est une ligne envoyée à CreatePatientsBulk.
type importJSON struct {
	First   string `json:"first"`
	Last    string `json:"last"`
	Birth   string `json:"birth"`
	Phone   string `json:"phone"`
	Email   string `json:"email"`
	Address string `json:"address"`
}

// Import ré-analyse le fichier puis insère, dans une seule transaction, les
// lignes valides non doublons (les doublons sont re-vérifiés dans la
// transaction). notifyOptIn : le cabinet atteste que ces patients acceptent les
// rappels ; sinon les deux consentements sont à faux.
func (s *PatientImport) Import(ctx context.Context, a Actor, data []byte, notifyOptIn bool) (ImportResult, error) {
	rows, err := ParseImportCSV(data)
	if err != nil {
		return ImportResult{}, err
	}
	var res ImportResult
	err = store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		lines, err := s.analyze(ctx, q, a, rows)
		if err != nil {
			return err
		}
		var batch []importJSON
		for _, l := range lines {
			if l.Status == ImportOK {
				batch = append(batch, importJSON{First: l.d.firstName, Last: l.d.lastName, Birth: birthString(l.d), Phone: l.d.phone, Email: l.d.email, Address: l.d.address})
			}
		}
		res = ImportResult{Imported: len(batch), Skipped: len(lines) - len(batch)}
		if len(batch) == 0 {
			return domain.ErrValidation{Field: "file", Msg: "Aucune ligne à importer : toutes sont invalides ou déjà présentes."}
		}
		payload, err := json.Marshal(batch)
		if err != nil {
			return fmt.Errorf("lot d'import: %w", err)
		}
		n, err := q.CreatePatientsBulk(ctx, sqlcgen.CreatePatientsBulkParams{PracticeID: a.PracticeID, OptIn: notifyOptIn, Rows: payload})
		if err != nil {
			return fmt.Errorf("insertion des patients: %w", err)
		}
		if int(n) != len(batch) {
			return fmt.Errorf("insertion des patients: %d lignes insérées sur %d", n, len(batch))
		}
		// Audit : l'importateur et des compteurs, jamais de donnée patient.
		meta, err := json.Marshal(map[string]string{"imported": fmt.Sprint(res.Imported), "skipped": fmt.Sprint(res.Skipped), "notify_opt_in": fmt.Sprint(notifyOptIn)})
		if err != nil {
			return fmt.Errorf("meta audit: %w", err)
		}
		if err := q.CreateAuditLog(ctx, sqlcgen.CreateAuditLogParams{
			UserID: pgtype.UUID{Bytes: a.UserID, Valid: true}, PracticeID: pgtype.UUID{Bytes: a.PracticeID, Valid: true},
			Actor: "user", Action: "patient.imported", Entity: "patient_import", Meta: meta,
		}); err != nil {
			return fmt.Errorf("journal d'audit: %w", err)
		}
		return nil
	})
	if err != nil {
		return ImportResult{}, err
	}
	return res, nil
}

func birthString(d details) string {
	if !d.birth.Valid {
		return ""
	}
	return d.birth.Time.Format("2006-01-02")
}

// analyze valide chaque ligne puis détecte les doublons (base, en une requête
// pour tout le fichier, puis intra-fichier : la première occurrence est gardée).
func (s *PatientImport) analyze(ctx context.Context, q *sqlcgen.Queries, a Actor, rows []ImportRow) ([]ImportLine, error) {
	now := s.Clock.Now()
	lines := make([]ImportLine, len(rows))
	var phones, emails []string
	for i, r := range rows {
		l := ImportLine{Line: r.Line, row: r, Status: ImportOK, Name: displayName(r)}
		if reason := validateImportRow(r, now, &l); reason != "" {
			l.Status, l.Reason = ImportInvalid, reason
		} else {
			l.Phone, l.Email = l.d.phone, l.d.email
			if l.Phone != "" {
				phones = append(phones, l.Phone)
			}
			if l.Email != "" {
				emails = append(emails, l.Email)
			}
		}
		lines[i] = l
	}

	inDB := map[string]bool{}
	if len(phones)+len(emails) > 0 {
		found, err := q.FindImportDuplicates(ctx, sqlcgen.FindImportDuplicatesParams{PracticeID: a.PracticeID, Phones: phones, Emails: emails})
		if err != nil {
			return nil, fmt.Errorf("recherche des doublons: %w", err)
		}
		for _, f := range found {
			if f.Phone != "" {
				inDB["p"+f.Phone] = true
			}
			if f.Email != "" {
				inDB["e"+strings.ToLower(f.Email)] = true
			}
		}
	}
	seen := map[string]int{} // contact → ligne de première occurrence importable
	for i := range lines {
		l := &lines[i]
		if l.Status != ImportOK {
			continue
		}
		switch {
		case l.Phone != "" && inDB["p"+l.Phone]:
			l.Status, l.Reason = ImportDuplicate, "Ce téléphone existe déjà dans les fiches du cabinet."
		case l.Email != "" && inDB["e"+l.Email]:
			l.Status, l.Reason = ImportDuplicate, "Cet email existe déjà dans les fiches du cabinet."
		}
		if l.Status == ImportOK {
			if first, ok := seen["p"+l.Phone]; ok && l.Phone != "" {
				l.Status, l.Reason = ImportDuplicate, fmt.Sprintf("Même téléphone que la ligne %d du fichier.", first)
			} else if first, ok := seen["e"+l.Email]; ok && l.Email != "" {
				l.Status, l.Reason = ImportDuplicate, fmt.Sprintf("Même email que la ligne %d du fichier.", first)
			}
		}
		if l.Status == ImportOK {
			if l.Phone != "" {
				seen["p"+l.Phone] = l.Line
			}
			if l.Email != "" {
				seen["e"+l.Email] = l.Line
			}
		}
	}
	return lines, nil
}

func displayName(r ImportRow) string {
	return strings.TrimSpace(strings.ToUpper(r.LastName) + " " + r.FirstName)
}

// validateImportRow retourne la raison du rejet, ou "" et remplit l.d.
func validateImportRow(r ImportRow, now time.Time, l *ImportLine) string {
	if r.BirthBad {
		return "Date de naissance illisible (JJ/MM/AAAA ou AAAA-MM-JJ)."
	}
	if r.Phone == "" && r.Email == "" {
		return "Ni téléphone ni email."
	}
	d, err := parseDetails(PatientDetails{
		FirstName: r.FirstName, LastName: r.LastName, Phone: r.Phone, Email: r.Email, BirthDate: r.Birth, Address: r.Address,
	}, channelFor(PatientDetails{Phone: r.Phone}), now)
	if err != nil {
		msgs := make([]string, 0, 2)
		for _, fe := range domain.FieldErrors(err) {
			msgs = append(msgs, fe.Msg)
		}
		return strings.Join(msgs, " ")
	}
	l.d = d
	return ""
}
