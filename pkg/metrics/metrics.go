// Package metrics is a thin wrapper over Prometheus client_golang: services
// declare counters/gauges with metrics.NewCounter/NewGauge and svc.Run serves
// the /metrics endpoint when METRICS_ADDR is set. Names should be prefixed
// "lobsterroll_<service>_".
package metrics

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Counter is a monotonically increasing metric.
type Counter interface {
	Inc()
	Add(float64)
}

// Gauge is a value that can go up or down.
type Gauge interface {
	Set(float64)
	Inc()
	Dec()
	Add(float64)
}

// NewCounter registers and returns a counter (idempotent registration panics on
// duplicate names, as Prometheus intends — declare each once as a package var).
func NewCounter(name, help string) Counter {
	return promauto.NewCounter(prometheus.CounterOpts{Name: name, Help: help})
}

// NewGauge registers and returns a gauge.
func NewGauge(name, help string) Gauge {
	return promauto.NewGauge(prometheus.GaugeOpts{Name: name, Help: help})
}

// Serve runs the /metrics endpoint on addr until ctx is cancelled, then shuts it
// down gracefully. Callers guard on a non-empty addr.
func Serve(ctx context.Context, addr string, log *slog.Logger) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		shctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shctx)
		close(done)
	}()

	log.Info("metrics serving", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	<-done
	return nil
}
