package web

import (
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"rsc.io/qr"

	"doctolibre/internal/httpx"
)

// quietZone est la zone de silence autour du QR code, en modules (minimum de la norme).
const quietZone = 4

// qrSVG encode text (niveau de correction M) et retourne un SVG autonome : un fond blanc
// et un seul <path> noir. Aucune donnée n'est injectée en texte : le SVG ne contient
// que des nombres produits ici, donc il peut être rendu tel quel. Noir sur blanc en dur :
// un QR code doit rester lisible quel que soit le thème.
func qrSVG(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", fmt.Errorf("qr: %w", err)
	}
	total := code.Size + 2*quietZone
	var b strings.Builder
	t := strconv.Itoa(total)
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ` + t + ` ` + t + `" role="img" aria-label="QR code de la page de réservation" shape-rendering="crispEdges">`)
	b.WriteString(`<rect width="` + t + `" height="` + t + `" fill="white"/><path fill="black" d="`)
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+quietZone, y+quietZone)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String(), nil
}

// PosterPage est la donnée de la page admin_poster.
type PosterPage struct {
	PracticeName  string
	Practitioners []string
	// URL est l'adresse publique ; vide si BaseURL n'est pas configuré (pas de QR).
	URL string
	// QR est le SVG produit par qrSVG (jamais une saisie).
	QR template.HTML
}

// posterRoutes enregistre /admin/poster, ouvert à tous les rôles du cabinet (comme le
// tableau de bord qui affiche déjà cette adresse).
func (d Deps) posterRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	if d.Practice == nil {
		return
	}
	require := d.Auth.RequireUser()
	base := strings.TrimRight(d.Config.BaseURL, "/")
	mux.Handle("GET /admin/poster", require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := actorFrom(r)
		dash, err := d.Practice.Dashboard(r.Context(), a)
		if err != nil {
			logger.Error("affiche: lecture en échec", slog.String("practice_id", a.PracticeID.String()), slog.String("err", err.Error()))
			rd.Error(w, r, http.StatusInternalServerError, "")
			return
		}
		page := PosterPage{PracticeName: dash.PracticeName}
		for _, p := range dash.Practitioners {
			page.Practitioners = append(page.Practitioners, strings.TrimSpace(p.FirstName+" "+p.LastName))
		}
		if base != "" {
			page.URL = base + "/p/" + url.PathEscape(dash.PracticeSlug)
			svg, err := qrSVG(page.URL)
			if err != nil {
				httpx.LoggerFrom(r.Context()).Error("affiche: QR en échec", slog.String("err", err.Error()))
				rd.Error(w, r, http.StatusInternalServerError, "")
				return
			}
			page.QR = template.HTML(svg) //nolint:gosec // SVG produit par qrSVG, sans texte libre
		}
		rd.Page(w, r, "admin_poster", page)
	})))
}
