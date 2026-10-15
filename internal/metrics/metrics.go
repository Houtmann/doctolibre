// Package metrics expose les métriques Prometheus de l'application.
//
// Le registre est propre à l'instance (pas de registre global). Les sources de
// données qui n'existent pas encore (outbox, pool pgx) se branchent via
// RegisterOutbox / RegisterPool sans que ce package dépende d'elles.
package metrics

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const unmatched = "unmatched"

// Metrics regroupe le registre et les métriques applicatives.
type Metrics struct {
	reg *prometheus.Registry
	// AppointmentsCreated est à incrémenter par le service de réservation
	// (label source : "online" ou "staff").
	AppointmentsCreated *prometheus.CounterVec
	requests            *prometheus.HistogramVec
}

// New crée le registre (collecteurs Go et process inclus).
func New() *Metrics {
	m := &Metrics{
		reg: prometheus.NewRegistry(),
		AppointmentsCreated: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "doctolibre_appointments_created_total",
			Help: "Rendez-vous créés, par source.",
		}, []string{"source"}),
		requests: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "doctolibre_http_request_duration_seconds",
			Help:    "Durée des requêtes HTTP par pattern de route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route", "status"}),
	}
	m.reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.AppointmentsCreated, m.requests,
	)
	return m
}

// Register ajoute un collecteur personnalisé au registre.
func (m *Metrics) Register(c prometheus.Collector) error { return m.reg.Register(c) }

// OutboxStats est l'état de la file de messages à l'instant de la collecte.
type OutboxStats struct {
	// ByStatus : nombre de messages par statut (pending, sent, failed…).
	ByStatus map[string]int64
	// OldestPending : âge du plus vieux message pending (0 si aucun).
	OldestPending time.Duration
}

// OutboxSource fournit les stats de l'outbox (à implémenter par le worker/store).
type OutboxSource interface {
	OutboxStats(ctx context.Context) (OutboxStats, error)
}

// RegisterOutbox expose les métriques de l'outbox, lues à chaque scrape.
func (m *Metrics) RegisterOutbox(src OutboxSource) error {
	return m.reg.Register(&outboxCollector{src: src})
}

type outboxCollector struct{ src OutboxSource }

var (
	outboxCountDesc = prometheus.NewDesc("doctolibre_outbox_messages", "Messages outbox par statut.", []string{"status"}, nil)
	outboxAgeDesc   = prometheus.NewDesc("doctolibre_outbox_oldest_pending_age_seconds", "Âge du plus vieux message pending.", nil, nil)
)

func (c *outboxCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- outboxCountDesc
	ch <- outboxAgeDesc
}

func (c *outboxCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	st, err := c.src.OutboxStats(ctx)
	if err != nil {
		ch <- prometheus.NewInvalidMetric(outboxCountDesc, err)
		return
	}
	for status, n := range st.ByStatus {
		ch <- prometheus.MustNewConstMetric(outboxCountDesc, prometheus.GaugeValue, float64(n), status)
	}
	ch <- prometheus.MustNewConstMetric(outboxAgeDesc, prometheus.GaugeValue, st.OldestPending.Seconds())
}

// PoolStats est un instantané du pool de connexions (champs de pgxpool.Stat).
type PoolStats struct {
	Acquired, Idle, Total, Max int32
}

// RegisterPool expose le pool pgx. fn typique :
//
//	func() metrics.PoolStats {
//		s := pool.Stat()
//		return metrics.PoolStats{Acquired: s.AcquiredConns(), Idle: s.IdleConns(), Total: s.TotalConns(), Max: s.MaxConns()}
//	}
func (m *Metrics) RegisterPool(fn func() PoolStats) error {
	return m.reg.Register(&poolCollector{fn: fn})
}

type poolCollector struct{ fn func() PoolStats }

var poolDescs = map[string]*prometheus.Desc{
	"acquired": prometheus.NewDesc("doctolibre_db_pool_acquired_conns", "Connexions DB en cours d'utilisation.", nil, nil),
	"idle":     prometheus.NewDesc("doctolibre_db_pool_idle_conns", "Connexions DB inactives.", nil, nil),
	"total":    prometheus.NewDesc("doctolibre_db_pool_total_conns", "Connexions DB ouvertes.", nil, nil),
	"max":      prometheus.NewDesc("doctolibre_db_pool_max_conns", "Taille maximale du pool DB.", nil, nil),
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range poolDescs {
		ch <- d
	}
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.fn()
	for k, v := range map[string]int32{"acquired": s.Acquired, "idle": s.Idle, "total": s.Total, "max": s.Max} {
		ch <- prometheus.MustNewConstMetric(poolDescs[k], prometheus.GaugeValue, float64(v))
	}
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Middleware mesure la durée des requêtes. Il doit envelopper DIRECTEMENT le mux :
// ServeMux renseigne r.Pattern sur la requête qu'il reçoit, invisible des
// middlewares externes qui ont copié la requête (RequestID). Le label route est
// le pattern ("GET /rdv/{id}"), jamais le chemin brut (cardinalité bornée).
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		route := r.Pattern
		if route == "" {
			route = unmatched
		}
		m.requests.WithLabelValues(r.Method, route, statusClass(rec.status)).Observe(time.Since(start).Seconds())
	})
}

func statusClass(code int) string {
	switch {
	case code >= 500:
		return "5xx"
	case code >= 400:
		return "4xx"
	case code >= 300:
		return "3xx"
	default:
		return "2xx"
	}
}

// Handler sert /metrics, protégé par un jeton Bearer (comparaison en temps
// constant). Un jeton vide ne doit pas arriver ici : l'appelant n'enregistre
// pas la route.
func (m *Metrics) Handler(token string) http.Handler {
	want := sha256.Sum256([]byte(token))
	inner := promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		sum := sha256.Sum256([]byte(got))
		if !ok || subtle.ConstantTimeCompare(sum[:], want[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "non autorisé", http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	})
}
