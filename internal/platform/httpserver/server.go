// Package httpserver runs an http.Handler until its context ends.
package httpserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Every phase of a connection is bounded, so a client that stalls cannot
// hold a connection or a handler forever.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// Serve answers requests on ln until ctx is canceled, then drains the
// requests in flight.
func Serve(ctx context.Context, ln net.Listener, handler http.Handler) error {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	failed := make(chan error, 1)
	go func() { failed <- srv.Serve(ln) }()

	select {
	case err := <-failed:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	// ctx is already canceled; the drain needs its own deadline.
	drain, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(drain); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}
