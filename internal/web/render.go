package web

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"doctolibre/internal/auth"
	"doctolibre/internal/httpx"
	"doctolibre/internal/service"
)

//go:embed templates static
var embedded embed.FS

// Répertoire des templates sur disque, utilisé pour le rechargement à chaud en dev.
const devTemplatesDir = "internal/web/templates"

// Flash est un message affiché une fois après une redirection.
type Flash struct {
	Kind    string `json:"k"` // success | error | info
	Message string `json:"m"`
}

// View est la donnée reçue par tous les templates : les champs communs plus la
// donnée propre à la page dans Data.
type View struct {
	CSRF  string
	Flash *Flash
	Data  any
	// Role est le rôle de l'utilisateur connecté ("" hors espace du cabinet) :
	// le menu de l'administration n'affiche que ce qu'il peut utiliser.
	Role string
	// Current est le lien du menu d'administration correspondant à la page
	// affichée ("" si aucun) : il porte aria-current="page".
	Current string
	// PrivacyURL est la page vie privée publique du cabinet de la page affichée
	// ("" si la route est inactive ou si la page n'est pas dans /p/{practice}).
	PrivacyURL string
}

// navCurrent retourne l'entrée du menu d'administration pour un chemin.
func navCurrent(path string) string {
	// L'ordre compte : le premier préfixe qui correspond gagne.
	for _, n := range [...]struct{ prefix, link string }{
		{"/admin/today", "/admin/today"},
		{"/admin/calendar", "/admin/calendar"},
		{"/admin/appointments", "/admin/calendar"},
		{"/admin/patients", "/admin/patients"},
		{"/admin/care-plans", "/admin/patients"},
		{"/admin/waitlist", "/admin/waitlist"},
		{"/admin/availability", "/admin/availability"},
		{"/admin/absences", "/admin/absences"},
		{"/admin/appointment-types", "/admin/appointment-types"},
		{"/admin/profile", "/admin/profile"},
		{"/admin/settings/feed", "/admin/profile"},
		{"/admin/settings", "/admin/settings"},
		{"/admin/privacy", "/admin/privacy"},
		{"/admin/team", "/admin/team"},
	} {
		if path == n.prefix || strings.HasPrefix(path, n.prefix+"/") {
			return n.link
		}
	}
	return ""
}

// roleOf retourne le rôle de l'utilisateur connecté de la requête.
func roleOf(req *http.Request) string {
	u, _ := auth.UserFrom(req.Context())
	return u.Role
}

// HXHeaders retourne la valeur de hx-headers (JSON) qui fait envoyer le jeton
// CSRF à chaque requête htmx, sans script inline.
func (v View) HXHeaders() string {
	b, _ := json.Marshal(map[string]string{httpx.CSRFHeader: v.CSRF})
	return string(b)
}

// RendererOptions configure un Renderer.
type RendererOptions struct {
	SecretKey string         // signe le cookie flash ; vide = clé aléatoire par process
	Location  *time.Location // fuseau d'affichage des dates (UTC si nil)
	Reload    bool           // dev : relit les templates à chaque rendu (depuis le disque si disponible)
	Secure    bool           // cookie flash Secure (prod)
	// PrivacyPublic : la page vie privée publique /p/{practice}/privacy est servie.
	PrivacyPublic bool
}

// Renderer rend les pages HTML et sert les fichiers statiques embarqués.
type Renderer struct {
	opts     RendererOptions
	flashKey []byte
	static   fs.FS
	versions map[string]string

	mu    sync.RWMutex
	sets  *templateSets
	funcs template.FuncMap
}

type templateSets struct {
	pages    map[string]*template.Template // "home" → ensemble base+layout+partials+page
	partials *template.Template
}

// NewRenderer parse tous les templates (une erreur de syntaxe échoue ici, pas au rendu).
func NewRenderer(opts RendererOptions) (*Renderer, error) {
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	key := []byte(opts.SecretKey)
	if len(key) == 0 {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("clé flash: %w", err)
		}
	}
	static, err := fs.Sub(embedded, "static")
	if err != nil {
		return nil, fmt.Errorf("static: %w", err)
	}
	r := &Renderer{opts: opts, flashKey: key, static: static}
	if r.versions, err = fileVersions(static); err != nil {
		return nil, err
	}
	r.funcs = r.funcMap()
	if r.sets, err = r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

func fileVersions(fsys fs.FS) (map[string]string, error) {
	out := map[string]string{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		out[p] = hex.EncodeToString(sum[:6])
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("empreintes static: %w", err)
	}
	return out, nil
}

var layoutRe = regexp.MustCompile(`^\{\{/\*\s*layout:\s*(\w+)\s*\*/\}\}`)

func (r *Renderer) templatesFS() fs.FS {
	if r.opts.Reload {
		if st, err := os.Stat(devTemplatesDir); err == nil && st.IsDir() {
			return os.DirFS(devTemplatesDir)
		}
	}
	sub, _ := fs.Sub(embedded, "templates")
	return sub
}

// load parse les templates. Une page peut choisir son layout avec, en première
// ligne, `{{/* layout: admin */}}` (défaut : public).
func (r *Renderer) load() (*templateSets, error) {
	fsys := r.templatesFS()
	read := func(p string) (string, error) {
		b, err := fs.ReadFile(fsys, p)
		return string(b), err
	}
	partialFiles, err := fs.Glob(fsys, "partials/*.html")
	if err != nil {
		return nil, err
	}
	pageFiles, err := fs.Glob(fsys, "pages/*.html")
	if err != nil {
		return nil, err
	}
	// base ajoute base + partials (+ layout) à un ensemble.
	build := func(layout string) (*template.Template, error) {
		t := template.New("").Funcs(r.funcs)
		files := append([]string{"layout/base.html"}, partialFiles...)
		if layout != "" {
			files = append(files, "layout/"+layout+".html")
		}
		for _, f := range files {
			src, err := read(f)
			if err != nil {
				return nil, fmt.Errorf("template %s: %w", f, err)
			}
			if _, err := t.New(path.Base(f)).Parse(src); err != nil {
				return nil, fmt.Errorf("template %s: %w", f, err)
			}
		}
		return t, nil
	}
	sets := &templateSets{pages: map[string]*template.Template{}}
	if sets.partials, err = build(""); err != nil {
		return nil, err
	}
	for _, f := range pageFiles {
		src, err := read(f)
		if err != nil {
			return nil, fmt.Errorf("template %s: %w", f, err)
		}
		layout := "public"
		if m := layoutRe.FindStringSubmatch(src); m != nil {
			layout = m[1]
		}
		t, err := build(layout)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(path.Base(f), ".html")
		if _, err := t.New("page:" + name).Parse(src); err != nil {
			return nil, fmt.Errorf("template %s: %w", f, err)
		}
		sets.pages[name] = t
	}
	return sets, nil
}

func (r *Renderer) current() (*templateSets, error) {
	if r.opts.Reload {
		sets, err := r.load()
		if err != nil {
			return nil, err
		}
		return sets, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sets, nil
}

// PageNames retourne les noms des pages connues (utile aux tests).
func (r *Renderer) PageNames() []string {
	names := make([]string, 0, len(r.sets.pages))
	for n := range r.sets.pages {
		names = append(names, n)
	}
	return names
}

// Page rend une page complète (layout inclus) avec le statut 200.
func (r *Renderer) Page(w http.ResponseWriter, req *http.Request, name string, data any) {
	r.PageStatus(w, req, http.StatusOK, name, data)
}

// PageStatus rend une page complète avec le statut donné. Le message flash en
// attente est consommé.
func (r *Renderer) PageStatus(w http.ResponseWriter, req *http.Request, status int, name string, data any) {
	if r == nil {
		httpx.Error(w, req, http.StatusInternalServerError, "erreur interne")
		return
	}
	sets, err := r.current()
	if err != nil {
		r.fail(w, req, "chargement des templates", err)
		return
	}
	t, ok := sets.pages[name]
	if !ok {
		r.fail(w, req, "page inconnue", fmt.Errorf("page %q", name))
		return
	}
	v := View{CSRF: httpx.CSRFToken(req.Context()), Flash: r.takeFlash(w, req), Data: data, Role: roleOf(req), Current: navCurrent(req.URL.Path)}
	if slug := req.PathValue("practice"); r.opts.PrivacyPublic && slug != "" {
		v.PrivacyURL = "/p/" + url.PathEscape(slug) + "/privacy"
	}
	r.write(w, req, status, t, "base.html", v)

}

// Partial rend un fragment (réponse htmx), sans layout. name est celui du
// fichier de partials/ sans extension (ex. "form_errors") ou d'un {{define}}.
// data est passée telle quelle au template (pas de View : si le fragment contient
// un formulaire, y inclure le jeton, via httpx.CSRFToken).
func (r *Renderer) Partial(w http.ResponseWriter, req *http.Request, name string, data any) {
	if r == nil {
		httpx.Error(w, req, http.StatusInternalServerError, "erreur interne")
		return
	}
	sets, err := r.current()
	if err != nil {
		r.fail(w, req, "chargement des templates", err)
		return
	}
	tname := name
	if sets.partials.Lookup(name+".html") != nil {
		tname = name + ".html"
	}
	r.write(w, req, http.StatusOK, sets.partials, tname, data)
}

func (r *Renderer) write(w http.ResponseWriter, req *http.Request, status int, t *template.Template, name string, data any) {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		r.fail(w, req, "rendu du template "+name, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func (r *Renderer) fail(w http.ResponseWriter, req *http.Request, what string, err error) {
	httpx.LoggerFrom(req.Context()).Error("rendu en échec", "what", what, "err", err.Error())
	httpx.Error(w, req, http.StatusInternalServerError, "erreur interne")
}

// ErrorData est la donnée de la page error.
type ErrorData struct {
	Status  int
	Title   string
	Message string
}

// Error rend la page d'erreur stylée (signature compatible avec httpx.RecoverWith).
func (r *Renderer) Error(w http.ResponseWriter, req *http.Request, status int, msg string) {
	if status == http.StatusTooManyRequests && req != nil && httpx.IsHTMX(req) {
		httpx.Error(w, req, status, msg) // fragment d'alerte, pas une page complète
		return
	}
	d := ErrorData{Status: status, Title: http.StatusText(status), Message: msg}
	switch {
	case status == http.StatusNotFound:
		d.Title, d.Message = "Page introuvable", "La page demandée n'existe pas ou a été déplacée."
	case status == http.StatusForbidden:
		d.Title = "Accès refusé"
	case status == http.StatusTooManyRequests:
		d.Title = "Trop de requêtes"
	case status == http.StatusServiceUnavailable && msg != "":
		d.Title = "Service indisponible" // message métier conservé (ex. réservation en ligne fermée)
	case status >= 500:
		d.Title, d.Message = "Une erreur est survenue", "Un problème est survenu de notre côté. Veuillez réessayer dans quelques instants."
	}
	if d.Message == "" {
		d.Message = d.Title
	}
	if r == nil {
		httpx.Error(w, req, status, msg)
		return
	}
	sets, err := r.current()
	if err != nil || sets.pages["error"] == nil {
		httpx.Error(w, req, status, msg)
		return
	}
	v := View{CSRF: httpx.CSRFToken(req.Context()), Data: d, Role: roleOf(req)}
	var buf bytes.Buffer
	if err := sets.pages["error"].ExecuteTemplate(&buf, "base.html", v); err != nil {
		httpx.Error(w, req, status, msg)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// --- Fichiers statiques ---

// StaticHandler sert /static/ : cache long (immutable) quand l'URL porte ?v=<empreinte>.
func (r *Renderer) StaticHandler() http.Handler {
	files := http.FileServerFS(r.static)
	h := http.StripPrefix("/static", files)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/") { // pas de listing de répertoire
			r.Error(w, req, http.StatusNotFound, "")
			return
		}
		if req.URL.Query().Get("v") != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		h.ServeHTTP(w, req)
	})
}

// AssetURL retourne /static/<name>?v=<empreinte> (empreinte calculée au démarrage).
func (r *Renderer) AssetURL(name string) string {
	if v, ok := r.versions[name]; ok {
		return "/static/" + name + "?v=" + v
	}
	return "/static/" + name
}

// --- Flash (cookie signé) ---

const flashCookie = "flash"

func (r *Renderer) sign(payload string) string {
	m := hmac.New(sha256.New, r.flashKey)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// SetFlash enregistre un message affiché au prochain rendu de page (kind : success, error, info).
func (r *Renderer) SetFlash(w http.ResponseWriter, kind, message string) {
	b, _ := json.Marshal(Flash{Kind: kind, Message: message})
	p := base64.RawURLEncoding.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure dépend de l'env (false en dev http)
		Name: flashCookie, Value: p + "." + r.sign(p), Path: "/", MaxAge: 300,
		HttpOnly: true, Secure: r.opts.Secure, SameSite: http.SameSiteLaxMode,
	})
}

var errBadFlash = errors.New("flash invalide")

func (r *Renderer) parseFlash(val string) (*Flash, error) {
	p, sig, ok := strings.Cut(val, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(r.sign(p))) {
		return nil, errBadFlash
	}
	raw, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return nil, errBadFlash
	}
	var f Flash
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, errBadFlash
	}
	switch f.Kind {
	case "success", "error", "info", "warning":
	default:
		f.Kind = "info"
	}
	return &f, nil
}

// takeFlash lit le flash éventuel et efface le cookie.
func (r *Renderer) takeFlash(w http.ResponseWriter, req *http.Request) *Flash {
	c, err := req.Cookie(flashCookie)
	if err != nil {
		return nil
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure dépend de l'env (false en dev http)
		Name: flashCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: r.opts.Secure, SameSite: http.SameSiteLaxMode,
	})
	f, err := r.parseFlash(c.Value)
	if err != nil {
		return nil
	}
	return f
}

// --- FuncMap ---

var (
	joursFR = [...]string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}
	moisFR  = [...]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}
)

func frenchDate(t time.Time) string {
	day := fmt.Sprint(t.Day())
	if t.Day() == 1 {
		day = "1er"
	}
	return fmt.Sprintf("%s %s %s %d", joursFR[t.Weekday()], day, moisFR[t.Month()-1], t.Year())
}

func (r *Renderer) funcMap() template.FuncMap {
	loc := r.opts.Location
	return template.FuncMap{
		// Numéro réduit aux chiffres et "+" pour un lien tel:.
		"telHref": telHref,
		// "mardi 15 octobre 2026", dans le fuseau du cabinet.
		"formatDate": func(t time.Time) string { return frenchDate(t.In(loc)) },
		// Idem, mais dans le fuseau porté par t (déjà celui du cabinet).
		"localDate": frenchDate,
		"localTime": func(t time.Time) string { return t.Format("15h04") },
		// Liens vers l'adresse d'une visite à domicile (schéma geo: pour les applis de cartes).
		"geoURL": func(addr string) template.URL { return template.URL("geo:0,0?q=" + url.QueryEscape(addr)) }, //nolint:gosec // schéma fixe, adresse échappée par QueryEscape
		"osmURL": func(addr string) string { return "https://www.openstreetmap.org/search?query=" + url.QueryEscape(addr) },
		// "14h30".
		"formatTime": func(t time.Time) string { return t.In(loc).Format("15h04") },
		// "45 min", "1 h", "1 h 30".
		"formatDuration": func(d time.Duration) string {
			m := int(d.Round(time.Minute) / time.Minute)
			switch {
			case m < 60:
				return fmt.Sprintf("%d min", m)
			case m%60 == 0:
				return fmt.Sprintf("%d h", m/60)
			default:
				return fmt.Sprintf("%d h %02d", m/60, m%60)
			}
		},
		"dict": func(kv ...any) (map[string]any, error) {
			if len(kv)%2 != 0 {
				return nil, errors.New("dict: nombre impair d'arguments")
			}
			m := make(map[string]any, len(kv)/2)
			for i := 0; i < len(kv); i += 2 {
				k, ok := kv[i].(string)
				if !ok {
					return nil, errors.New("dict: clé non textuelle")
				}
				m[k] = kv[i+1]
			}
			return m, nil
		},
		// {{csrf .CSRF}} : champ caché à placer dans chaque formulaire POST.
		"csrf": func(token string) template.HTML {
			return template.HTML(`<input type="hidden" name="` + httpx.CSRFFormField + `" value="` + html.EscapeString(token) + `">`) //nolint:gosec // valeur échappée, nom constant
		},
		"asset": r.AssetURL,
		// "Lundi" pour 0 … "Dimanche" pour 6 (convention availability_rule.weekday).
		"weekdayName": func(w int) string { return service.Capitalize(service.WeekdayName(w)) },
	}
}
