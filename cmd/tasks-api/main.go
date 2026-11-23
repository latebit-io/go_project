// Command tasks-api serves the example tasks context over HTTP.
package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/latebit-io/go_project/internal/platform/httpserver"
	"github.com/latebit-io/go_project/internal/tasks/adapters/memory"
	"github.com/latebit-io/go_project/internal/tasks/adapters/rest"
	"github.com/latebit-io/go_project/internal/tasks/app"
	"github.com/latebit-io/go_project/internal/tasks/domain"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tasks-api:", err)
		os.Exit(1)
	}
}

// run is the composition root: it builds adapters and hands them to the use
// cases, then serves until interrupted. TestSmoke starts it as a real process.
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	addr := cmp.Or(os.Getenv("TASKS_ADDR"), "127.0.0.1:8080")

	var listen net.ListenConfig

	ln, err := listen.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	now := func() time.Time { return time.Now().UTC() }
	newID := func() domain.ID { return domain.ID(rand.Text()) }
	service := app.NewService(memory.NewRepository(), now, newID)

	logger.InfoContext(ctx, "listening", slog.String("addr", ln.Addr().String()))

	if err := httpserver.Serve(ctx, ln, rest.NewHandler(service, logger)); err != nil {
		return fmt.Errorf("serve tasks: %w", err)
	}

	return nil
}
