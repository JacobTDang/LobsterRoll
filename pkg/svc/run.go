// Package svc provides a shared service lifecycle: structured logging plus a
// signal-cancelled context so every binary shuts down gracefully the same way.
package svc

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// detachedTimeout bounds work that outlives shutdown cancellation.
const detachedTimeout = 30 * time.Second

// Detached returns a context divorced from ctx's cancellation but bounded by a
// timeout, so an in-flight side effect (a NATS publish, a Telegram round-trip)
// can finish during the graceful-shutdown drain instead of being cut off. The
// caller must defer the returned cancel.
func Detached(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), detachedTimeout)
}

// Run wires up a JSON logger and a context cancelled on SIGINT/SIGTERM, then
// invokes fn. fn should block until ctx is done and return nil on clean
// shutdown, or an error to exit non-zero.
func Run(name string, fn func(ctx context.Context, log *slog.Logger) error) {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})).With("service", name)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info("starting")
	if err := fn(ctx, log); err != nil {
		log.Error("exited with error", "err", err)
		os.Exit(1)
	}
	log.Info("stopped")
}
