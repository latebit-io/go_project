package rest_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/latebit-io/go_project/internal/tasks/adapters/memory"
	"github.com/latebit-io/go_project/internal/tasks/adapters/rest"
	"github.com/latebit-io/go_project/internal/tasks/app"
	"github.com/latebit-io/go_project/internal/tasks/domain"
)

const (
	tasksPath    = "/tasks"
	completePath = "/tasks/t1/complete"
	failed       = http.StatusInternalServerError
)

var errBroken = errors.New("disk on fire")

type brokenRepository struct{}

func (brokenRepository) Save(context.Context, *domain.Task) error { return errBroken }

func (brokenRepository) Update(context.Context, domain.ID, func(*domain.Task) error) (*domain.Task, error) {
	return nil, errBroken
}

func (brokenRepository) List(context.Context) ([]*domain.Task, error) { return nil, errBroken }

// step is one request and what the response must look like.
type step struct {
	method string
	target string
	body   string
	want   int
	// contains must be in the response body; lacks must not be.
	contains string
	lacks    string
}

// post is a POST whose response must have the status and contain the text.
func post(target, body string, want int, contains string) step {
	return step{method: http.MethodPost, target: target, body: body, want: want, contains: contains}
}

func list(want int, contains string) step {
	return step{method: http.MethodGet, target: tasksPath, want: want, contains: contains}
}

func newHandler(repo domain.Repository) http.Handler {
	now := func() time.Time { return time.Date(2026, time.January, 2, 9, 0, 0, 0, time.UTC) }
	service := app.NewService(repo, now, func() domain.ID { return "t1" })

	return rest.NewHandler(service, slog.New(slog.DiscardHandler))
}

func TestHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// repo defaults to an empty in-memory repository.
		repo domain.Repository
		// steps run in order against one handler.
		steps []step
	}{
		{
			name: "a task is added, completed once and listed",
			steps: []step{
				post(tasksPath, `{"title":" write docs "}`, http.StatusCreated, `"title":"write docs","done":false`),
				post(completePath, "", http.StatusOK, `"done":true`),
				post(completePath, "", http.StatusConflict, ""),
				list(http.StatusOK, `"completedAt"`),
			},
		},
		{name: "an empty list is an array", steps: []step{list(http.StatusOK, "[]")}},
		{name: "malformed body", steps: []step{post(tasksPath, `{`, http.StatusBadRequest, "")}},
		{name: "unknown field", steps: []step{post(tasksPath, `{"name":"x"}`, http.StatusBadRequest, "")}},
		{name: "data after the object", steps: []step{post(tasksPath, `{"title":"x"} junk`, http.StatusBadRequest, "")}},
		{name: "second object", steps: []step{post(tasksPath, `{"title":"x"}{"title":"y"}`, http.StatusBadRequest, "")}},
		{name: "blank title", steps: []step{post(tasksPath, `{"title":" "}`, http.StatusUnprocessableEntity, "")}},
		{name: "unknown task", steps: []step{post("/tasks/nope/complete", "", http.StatusNotFound, "")}},
		{
			name: "storage failure on add", repo: brokenRepository{},
			steps: []step{post(tasksPath, `{"title":"x"}`, failed, "")},
		},
		{
			name: "internal errors stay out of the response", repo: brokenRepository{},
			steps: []step{{method: http.MethodGet, target: tasksPath, want: failed, lacks: errBroken.Error()}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := tt.repo
			if repo == nil {
				repo = memory.NewRepository()
			}

			handler := newHandler(repo)

			for _, s := range tt.steps {
				rec := httptest.NewRecorder()
				req := httptest.NewRequestWithContext(t.Context(), s.method, s.target, strings.NewReader(s.body))
				handler.ServeHTTP(rec, req)

				got := rec.Body.String()
				leaked := s.lacks != "" && strings.Contains(got, s.lacks)

				if rec.Code != s.want || !strings.Contains(got, s.contains) || leaked {
					t.Fatalf("%s %s = %d %s, want %+v", s.method, s.target, rec.Code, got, s)
				}
			}
		})
	}
}
