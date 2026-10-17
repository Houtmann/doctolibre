package web

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/service"
)

const (
	pathFeedSettings = pathSettings + "/feed"
	feedSuffix       = ".ics"
	feedCacheControl = "private, max-age=300"
)

// feedRoutes enregistre le flux public GET /ics/{token}.ics (le jeton fait foi,
// pas de session).
func (d Deps) feedRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	h := &feedHandlers{svc: d.Feed, rd: rd, logger: logger}
	mux.HandleFunc("GET /ics/{file}", h.serve)
}

type feedHandlers struct {
	svc    *service.CalendarFeed
	rd     *Renderer
	logger *slog.Logger
}

func (h *feedHandlers) serve(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutSuffix(r.PathValue("file"), feedSuffix)
	if !ok || token == "" {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	body, etag, err := h.svc.Render(r.Context(), token)
	if errors.Is(err, domain.ErrNotFound) {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	if err != nil {
		httpx.LoggerFrom(r.Context()).Error("flux ICS en échec", slog.String("err", err.Error()))
		h.rd.Error(w, r, http.StatusInternalServerError, "")
		return
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", feedCacheControl)
	if match := r.Header.Get("If-None-Match"); match != "" && etagMatches(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	_, _ = w.Write(body) //nolint:gosec // text/calendar, pas du HTML ; contenu échappé par le paquet ics
}

func etagMatches(header, etag string) bool {
	for _, c := range strings.Split(header, ",") {
		c = strings.TrimPrefix(strings.TrimSpace(c), "W/")
		if c == etag || c == "*" {
			return true
		}
	}
	return false
}

// --- Réglages de l'abonnement (page « Mon profil » ; chemins conservés sous /admin/settings/feed) ---

// FeedPanel est la part « abonnement agenda » de la page du profil.
type FeedPanel struct {
	Enabled   bool
	FullNames bool
	URL       string // renseigné uniquement juste après (re)génération : le jeton n'est plus récupérable ensuite
}

func (h *configHandlers) feedPanel(r *http.Request) (*FeedPanel, error) {
	if h.feed == nil {
		return nil, nil
	}
	st, err := h.feed.Status(r.Context(), actorFrom(r))
	if err != nil {
		return nil, err
	}
	return &FeedPanel{Enabled: st.Enabled, FullNames: st.FullNames}, nil
}

func (h *configHandlers) feedGenerate(w http.ResponseWriter, r *http.Request) {
	full := r.PostFormValue("full_names") == "1"
	token, err := h.feed.Regenerate(r.Context(), actorFrom(r), full)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	pr, err := h.svc.Practitioner(r.Context(), actorFrom(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	h.rd.Page(w, r, "admin_profile", ProfilePage{
		Form: profileInputOf(pr),
		Feed: &FeedPanel{
			Enabled: true, FullNames: full,
			URL: strings.TrimRight(h.baseURL, "/") + "/ics/" + url.PathEscape(token) + feedSuffix,
		},
	})
}

func (h *configHandlers) feedOptions(w http.ResponseWriter, r *http.Request) {
	err := h.feed.SetFullNames(r.Context(), actorFrom(r), r.PostFormValue("full_names") == "1")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.SetFlash(w, "success", "Option enregistrée.")
	httpx.Redirect(w, r, pathProfile)
}

func (h *configHandlers) feedRevoke(w http.ResponseWriter, r *http.Request) {
	if err := h.feed.Revoke(r.Context(), actorFrom(r)); err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.SetFlash(w, "success", "Abonnement révoqué : l'ancien lien ne fonctionne plus.")
	httpx.Redirect(w, r, pathProfile)
}
