package rest_test

import (
	"context"
	"encoding/json"
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

const tasksPath = "/tasks"

type taskBody struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Done        bool       `json:"done"`
	CompletedAt *time.Time `json:"completedAt"`
}

type brokenRepository struct{}

var errBroken = errors.New("disk on fire")

func (brokenRepository) Save(context.Context, *domain.Task) error { return errBroken }

func (brokenRepository) Find(context.Context, domain.ID) (*domain.Task, error) {
	return nil, errBroken
}

func (brokenRepository) List(context.Context) ([]*domain.Task, error) { return nil, errBroken }

func newHandler(repo domain.Repository) http.Handler {
	now := func() time.Time { return time.Date(2026, time.January, 2, 9, 0, 0, 0, time.UTC) }
	service := app.NewService(repo, now, func() domain.ID { return "t1" })

	return rest.NewHandler(service, slog.New(slog.DiscardHandler))
}

type request struct {
	method string
	target string
	body   string
}

func post(target, body string) request {
	return request{method: http.MethodPost, target: target, body: body}
}

func list() request {
	return request{method: http.MethodGet, target: tasksPath}
}

func call(t *testing.T, handler http.Handler, req request) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	body := strings.NewReader(req.body)
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), req.method, req.target, body))

	return rec
}

// decode fails the test unless rec has the wanted status, then reads its body.
func decode(t *testing.T, rec *httptest.ResponseRecorder, want int, into any) {
	t.Helper()

	if rec.Code != want {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, want, rec.Body.String())
	}

	if err := json.NewDecoder(rec.Body).Decode(into); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func TestTaskLifecycle(t *testing.T) {
	t.Parallel()

	handler := newHandler(memory.NewRepository())

	var added, completed taskBody

	decode(t, call(t, handler, post(tasksPath, `{"title":" write docs "}`)), http.StatusCreated, &added)

	if added.ID != "t1" || added.Title != "write docs" || added.Done {
		t.Fatalf("added = %+v, want open task t1 titled write docs", added)
	}

	decode(t, call(t, handler, post("/tasks/t1/complete", "")), http.StatusOK, &completed)

	if !completed.Done || completed.CompletedAt == nil {
		t.Fatalf("completed = %+v, want a done task with a completion time", completed)
	}

	var listed []taskBody

	decode(t, call(t, handler, list()), http.StatusOK, &listed)

	if len(listed) != 1 || !listed[0].Done {
		t.Fatalf("listed = %+v, want one done task", listed)
	}
}

func TestEmptyListIsAnArray(t *testing.T) {
	t.Parallel()

	rec := call(t, newHandler(memory.NewRepository()), list())
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("GET /tasks body = %s, want []", got)
	}
}

func TestErrorsMapToStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		repo domain.Repository
		req  request
		want int
	}{
		{name: "malformed body", req: post(tasksPath, `{`), want: http.StatusBadRequest},
		{name: "unknown field", req: post(tasksPath, `{"name":"x"}`), want: http.StatusBadRequest},
		{name: "blank title", req: post(tasksPath, `{"title":" "}`), want: http.StatusUnprocessableEntity},
		{name: "unknown task", req: post("/tasks/nope/complete", ""), want: http.StatusNotFound},
		{
			name: "storage failure on add", repo: brokenRepository{},
			req: post(tasksPath, `{"title":"x"}`), want: http.StatusInternalServerError,
		},
		{
			name: "storage failure on list", repo: brokenRepository{},
			req: list(), want: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := tt.repo
			if repo == nil {
				repo = memory.NewRepository()
			}

			if rec := call(t, newHandler(repo), tt.req); rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestCompletingTwiceConflicts(t *testing.T) {
	t.Parallel()

	handler := newHandler(memory.NewRepository())
	call(t, handler, post(tasksPath, `{"title":"write docs"}`))
	call(t, handler, post("/tasks/t1/complete", ""))

	if rec := call(t, handler, post("/tasks/t1/complete", "")); rec.Code != http.StatusConflict {
		t.Fatalf("second complete status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestInternalErrorsStayOutOfTheResponse(t *testing.T) {
	t.Parallel()

	rec := call(t, newHandler(brokenRepository{}), list())
	if strings.Contains(rec.Body.String(), errBroken.Error()) {
		t.Fatalf("response leaks the internal error: %s", rec.Body.String())
	}
}
