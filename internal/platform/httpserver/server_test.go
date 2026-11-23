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

// status sends one GET to the address and returns the response status.
func status(t *testing.T, addr net.Addr) int {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr.String(), http.NoBody)
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

	return resp.StatusCode
}

// serving starts Serve on the listener and returns the channel its result arrives on.
func serving(ctx context.Context, ln net.Listener) <-chan error {
	noContent := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	done := make(chan error, 1)
	go func() { done <- httpserver.Serve(ctx, ln, noContent) }()

	return done
}

// answerThenCancel sends one request to a running server, then cancels it.
func answerThenCancel(t *testing.T) error {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ln := listen(t)
	done := serving(ctx, ln)

	if got := status(t, ln.Addr()); got != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", got, http.StatusNoContent)
	}

	cancel()

	return <-done
}

// closedListener gives Serve a listener that cannot accept.
func closedListener(t *testing.T) error {
	t.Helper()

	ln := listen(t)
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	return <-serving(t.Context(), ln)
}

func TestServe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		run     func(t *testing.T) error
		wantErr bool
	}{
		{name: "answers until canceled, then stops cleanly", run: answerThenCancel},
		{name: "reports a listener failure", run: closedListener, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := tt.run(t); (err != nil) != tt.wantErr {
				t.Fatalf("Serve() error = %v, want error: %v", err, tt.wantErr)
			}
		})
	}
}
