package httpx

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newClock() *fakeClock { return &fakeClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)} }

func headerKey(r *http.Request) string { return r.Header.Get("X-Key") }

func call(h http.Handler, key string, htmx bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Key", key)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLimiterBurstAndRefill(t *testing.T) {
	t.Parallel()
	clk := newClock()
	l := NewLimiter("test", headerKey, 1, 2)
	l.now = clk.Now
	h := l.Middleware()(http.HandlerFunc(ok))

	for i := range 2 {
		if c := call(h, "a", false).Code; c != 200 {
			t.Fatalf("requête %d de la rafale: %d", i+1, c)
		}
	}
	if c := call(h, "a", false).Code; c != http.StatusTooManyRequests {
		t.Errorf("3e requête: %d, attendu 429", c)
	}
	if call(h, "b", false).Code != 200 {
		t.Error("une autre clé ne doit pas être limitée")
	}
	clk.Add(2 * time.Second)
	if call(h, "a", false).Code != 200 {
		t.Error("le jeton doit se régénérer")
	}
}

func TestLimiterSharedBetweenRoutes(t *testing.T) {
	t.Parallel()
	clk := newClock()
	l := NewLimiter("shared", headerKey, 0.001, 2)
	l.now = clk.Now
	mw := l.Middleware()
	mux := http.NewServeMux()
	mux.Handle("GET /a", mw(http.HandlerFunc(ok)))
	mux.Handle("GET /b", mw(http.HandlerFunc(ok)))
	get := func(path string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Key", "k")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	if get("/a") != 200 || get("/b") != 200 {
		t.Fatal("deux requêtes sur deux routes: la rafale de 2 est partagée")
	}
	if c := get("/a"); c != http.StatusTooManyRequests {
		t.Errorf("3e requête (route a): %d, attendu 429 (seau partagé)", c)
	}
	if c := get("/b"); c != http.StatusTooManyRequests {
		t.Errorf("3e requête (route b): %d, attendu 429 (seau partagé)", c)
	}
}

func TestLimiterRetryAfter(t *testing.T) {
	t.Parallel()
	clk := newClock()
	// 1 jeton toutes les 30 s, rafale 1.
	l := NewLimiter("slow", headerKey, 1.0/30, 1)
	l.now = clk.Now
	h := l.Middleware()(http.HandlerFunc(ok))
	call(h, "a", false)
	rec := call(h, "a", false)
	if rec.Code != 429 {
		t.Fatalf("code %d", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "30" {
		t.Errorf("Retry-After = %q, attendu 30", got)
	}
	clk.Add(10 * time.Second)
	if got := call(h, "a", false).Header().Get("Retry-After"); got != "20" {
		t.Errorf("Retry-After = %q, attendu 20", got)
	}
	// Un refus ne consomme rien : à t+30 s le jeton est là.
	clk.Add(20 * time.Second)
	if call(h, "a", false).Code != 200 {
		t.Error("le jeton devrait être revenu après 30 s")
	}
	// Délai inférieur à 1 s : arrondi à 1.
	fast := NewLimiter("fast", headerKey, 10, 1)
	fast.now = clk.Now
	fh := fast.Middleware()(http.HandlerFunc(ok))
	call(fh, "a", false)
	if got := call(fh, "a", false).Header().Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q, attendu 1", got)
	}
}

func TestLimiterHTMXFragment(t *testing.T) {
	t.Parallel()
	l := NewLimiter("htmx", headerKey, 0.001, 1)
	h := l.Middleware()(http.HandlerFunc(ok))
	call(h, "a", true)
	rec := call(h, "a", true)
	if rec.Code != 429 {
		t.Fatalf("code %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="alert alert-warning"`) || !strings.Contains(body, "Trop de requêtes, réessayez dans") {
		t.Errorf("fragment attendu, reçu %q", body)
	}
	if strings.Contains(body, "<html") || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("ni page complète ni content-type html: %q / %q", body, rec.Header().Get("Content-Type"))
	}
	// Sans htmx : texte brut.
	plain := call(h, "a", false)
	if strings.Contains(plain.Body.String(), "<div") {
		t.Errorf("pas de fragment hors htmx: %q", plain.Body.String())
	}
}

func TestLimiterMetricHook(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var got []string
	l := NewLimiter("hooked", headerKey, 0.001, 1).OnLimit(func(name string) {
		mu.Lock()
		got = append(got, name)
		mu.Unlock()
	})
	h := l.Middleware()(http.HandlerFunc(ok))
	call(h, "a", false)
	call(h, "a", false)
	call(h, "a", false)
	if len(got) != 2 || got[0] != "hooked" {
		t.Errorf("hook appelé %v, attendu 2 fois avec le nom du limiteur", got)
	}
}

func TestLimiterReservationCancel(t *testing.T) {
	t.Parallel()
	clk := newClock()
	l := NewLimiter("res", nil, 0.001, 1)
	l.now = clk.Now
	res := l.Reserve("p")
	if !res.OK() {
		t.Fatal("premier jeton refusé")
	}
	res.Cancel()
	if ok, _ := l.AllowKey("p"); !ok {
		t.Error("le jeton rendu par Cancel doit être réutilisable")
	}
	if ok, _ := l.AllowKey("p"); ok {
		t.Error("le seau devrait être vide")
	}
}

func TestLimiterBoundedMemory(t *testing.T) {
	t.Parallel()
	clk := newClock()
	// Quasi pas de régénération : les seaux restent entamés, donc non évincables.
	l := NewLimiter("bounded", headerKey, 0.0001, 1)
	l.now = clk.Now
	l.maxVisitors = 3
	for i := range 3 {
		if ok, _ := l.AllowKey(fmt.Sprint("k", i)); !ok {
			t.Fatalf("clé %d refusée", i)
		}
	}
	// 4e clé : map pleine, balayage sans effet → seau de débordement (une rafale pour tous).
	if ok, _ := l.AllowKey("new1"); !ok {
		t.Error("le premier passage dans le seau de débordement devrait passer")
	}
	if ok, _ := l.AllowKey("new2"); ok {
		t.Error("le seau de débordement est partagé: la 2e clé nouvelle est refusée")
	}
	l.mu.Lock()
	n := len(l.visitors)
	l.mu.Unlock()
	if n != 3 {
		t.Errorf("la map a grossi: %d clés", n)
	}
	// Les clés connues gardent leur seau propre.
	if ok, _ := l.AllowKey("k0"); ok {
		t.Error("k0 a déjà consommé sa rafale")
	}
	// Une clé réelle nommée « overflow » ne collisionne pas avec le seau de débordement.
	l2 := NewLimiter("collide", headerKey, 0.0001, 1)
	l2.now = clk.Now
	if ok, _ := l2.AllowKey("overflow"); !ok {
		t.Error("clé « overflow » ordinaire refusée")
	}
}

func TestLimiterSweepEvictsRefilledAndIdle(t *testing.T) {
	t.Parallel()
	clk := newClock()
	l := NewLimiter("sweep", headerKey, 1, 2)
	l.now = clk.Now
	l.maxVisitors = 2
	l.AllowKey("a")
	l.AllowKey("b")
	// Les seaux se remplissent à nouveau : à la prochaine clé neuve, le balayage forcé libère de la place.
	clk.Add(5 * time.Second)
	if ok, _ := l.AllowKey("c"); !ok {
		t.Fatal("clé neuve refusée")
	}
	l.mu.Lock()
	_, hasC := l.visitors["c"]
	n := len(l.visitors)
	l.mu.Unlock()
	if !hasC || n > 2 {
		t.Errorf("c devrait avoir sa place dans la map (n=%d, c=%v)", n, hasC)
	}
	// Balayage périodique : une clé inactive depuis plus de 10 min disparaît.
	clk.Add(11 * time.Minute)
	l.AllowKey("d")
	l.mu.Lock()
	_, hasC = l.visitors["c"]
	l.mu.Unlock()
	if hasC {
		t.Error("c, inactif depuis 11 min, aurait dû être évincé")
	}
}

func TestLimiterConcurrent(t *testing.T) {
	t.Parallel()
	l := NewLimiter("race", nil, 1000, 50)
	l.maxVisitors = 20
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 500 {
				res := l.Reserve(fmt.Sprint("k", (g*7+i)%100))
				if i%3 == 0 {
					res.Cancel()
				}
			}
		}()
	}
	wg.Wait()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.visitors) > 20 {
		t.Errorf("map non bornée: %d", len(l.visitors))
	}
}

func TestIPKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		remote, want string
	}{
		{"192.0.2.1:1234", "192.0.2.1"},
		{"[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:80", "2001:db8:1:2::/64"},
		{"[2001:db8:1:2:1111:2222:3333:4444]:80", "2001:db8:1:2::/64"},
		{"[2001:db8:1:3::1]:80", "2001:db8:1:3::/64"},
		{"[::ffff:192.0.2.9]:80", "192.0.2.9"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = tt.remote
		if got := IPKey(req); got != tt.want {
			t.Errorf("IPKey(%s) = %q, attendu %q", tt.remote, got, tt.want)
		}
	}
}
