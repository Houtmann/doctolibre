package web

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/service"
)

const pathPatientImport = pathPatients + "/import"

// patientImportRoutes enregistre l'import CSV (tous les rôles du cabinet).
func (d Deps) patientImportRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	h := &patientImportHandlers{svc: d.PatientImport, rd: rd, logger: logger}
	require := d.Auth.RequireUser(domain.RoleAdmin, domain.RolePractitioner, domain.RoleSecretary)
	mux.Handle("GET "+pathPatientImport, require(http.HandlerFunc(h.form)))
	mux.Handle("POST "+pathPatientImport, require(http.HandlerFunc(h.submit)))
}

type patientImportHandlers struct {
	svc    *service.PatientImport
	rd     *Renderer
	logger *slog.Logger
}

// PatientImportPage est la donnée de la page admin_patient_import.
type PatientImportPage struct {
	Errors  []string
	Preview *service.ImportPreview
	Payload string // fichier renvoyé tel quel à la confirmation (base64)
	MaxMB   int
	MaxRows int
}

func newImportPage() PatientImportPage {
	return PatientImportPage{MaxMB: service.MaxImportBytes >> 20, MaxRows: service.MaxImportRows}
}

func (h *patientImportHandlers) form(w http.ResponseWriter, r *http.Request) {
	h.rd.Page(w, r, "admin_patient_import", newImportPage())
}

// submit gère les deux étapes : sans « confirm », aperçu du fichier téléversé ;
// avec « confirm=1 », import du fichier renvoyé dans le champ caché « payload ».
func (h *patientImportHandlers) submit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, httpx.MaxMultipartLen)
	confirm := r.PostFormValue("confirm") == "1"
	data, err := h.readData(r, confirm)
	if err != nil {
		h.invalid(w, r, err)
		return
	}
	a := actorFrom(r)
	if !confirm {
		pv, err := h.svc.Preview(r.Context(), a, data)
		if err != nil {
			h.invalid(w, r, err)
			return
		}
		page := newImportPage()
		page.Preview = &pv
		page.Payload = base64.StdEncoding.EncodeToString(data)
		h.rd.Page(w, r, "admin_patient_import", page)
		return
	}
	res, err := h.svc.Import(r.Context(), a, data, r.PostFormValue("notify_opt_in") == "1")
	if err != nil {
		h.invalid(w, r, err)
		return
	}
	h.logger.Info("import de patients", slog.String("user_id", a.UserID.String()),
		slog.Int("imported", res.Imported), slog.Int("skipped", res.Skipped))
	h.rd.SetFlash(w, "success", fmt.Sprintf("%d patient(s) importé(s), %d ligne(s) ignorée(s).", res.Imported, res.Skipped))
	httpx.Redirect(w, r, pathPatients)
}

// readData lit le fichier téléversé (étape 1) ou renvoyé (étape 2), borné à 2 Mio.
func (h *patientImportHandlers) readData(r *http.Request, confirm bool) ([]byte, error) {
	tooBig := domain.ErrValidation{Field: "file", Msg: fmt.Sprintf("Le fichier dépasse %d Mo.", service.MaxImportBytes>>20)}
	if confirm {
		raw := r.PostFormValue("payload")
		if base64.StdEncoding.DecodedLen(len(raw)) > service.MaxImportBytes+3 {
			return nil, tooBig
		}
		data, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(data) == 0 {
			return nil, domain.ErrValidation{Field: "file", Msg: "Le fichier n'a pas pu être relu : recommencez le téléversement."}
		}
		if len(data) > service.MaxImportBytes {
			return nil, tooBig
		}
		return data, nil
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return nil, tooBig
		}
		return nil, domain.ErrValidation{Field: "file", Msg: "Choisissez un fichier CSV."}
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, service.MaxImportBytes+1))
	if err != nil {
		return nil, fmt.Errorf("lecture du fichier: %w", err)
	}
	if len(data) > service.MaxImportBytes {
		return nil, tooBig
	}
	return data, nil
}

func (h *patientImportHandlers) invalid(w http.ResponseWriter, r *http.Request, err error) {
	if msgs := validation(err); msgs != nil {
		page := newImportPage()
		page.Errors = msgs
		h.rd.PageStatus(w, r, http.StatusUnprocessableEntity, "admin_patient_import", page)
		return
	}
	a := actorFrom(r)
	httpx.LoggerFrom(r.Context()).Error("import de patients en échec",
		slog.String("user_id", a.UserID.String()), slog.String("err", err.Error()))
	h.rd.Error(w, r, http.StatusInternalServerError, "")
}
