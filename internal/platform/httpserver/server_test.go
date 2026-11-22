package httpserver_test

import (
	"context"
	"net"
	"net/http"
	"testing"

	"github.com/latebit-io/go_project/internal/platform/httpserver"
)

func listen(t *testing.T) net.Listener {
	t.Helper()

	var config net.ListenConfig

	ln, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	return ln
}

func TestServeAnswersUntilCanceled(t *testing.T) {
	t.Parallel()

	ln := listen(t)
	ctx, cancel := context.WithCancel(t.Context())
	noContent := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	done := make(chan error, 1)
	go func() { done <- httpserver.Serve(ctx, ln, noContent) }()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+ln.Addr().String(), http.NoBody)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET error = %v", err)
	}

	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	cancel()

	if err := <-done; err != nil {
		t.Fatalf("Serve() error = %v, want nil after cancel", err)
	}
}

func TestServeReportsListenerFailure(t *testing.T) {
	t.Parallel()

	ln := listen(t)
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	if err := httpserver.Serve(t.Context(), ln, http.NotFoundHandler()); err == nil {
		t.Fatal("Serve() error = nil, want the listener failure")
	}
}
