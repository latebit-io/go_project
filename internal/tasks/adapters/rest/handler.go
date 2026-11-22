// Package rest exposes the task use cases over HTTP and JSON.
package rest

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/latebit-io/go_project/internal/tasks/app"
	"github.com/latebit-io/go_project/internal/tasks/domain"
)

const maxBodyBytes = 1 << 20

var errInvalidBody = errors.New("request body is not the expected JSON")

type addRequest struct {
	Title string `json:"title"`
}

type taskResponse struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Done        bool       `json:"done"`
	CreatedAt   time.Time  `json:"createdAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type handler struct {
	tasks  *app.Service
	logger *slog.Logger
}

// NewHandler routes task requests to the service.
func NewHandler(tasks *app.Service, logger *slog.Logger) http.Handler {
	h := handler{tasks: tasks, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /tasks", h.add)
	mux.HandleFunc("GET /tasks", h.list)
	mux.HandleFunc("POST /tasks/{id}/complete", h.complete)

	return mux
}

func (h handler) add(w http.ResponseWriter, r *http.Request) {
	var req addRequest

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&req); err != nil {
		h.fail(w, r, errInvalidBody)

		return
	}

	task, err := h.tasks.Add(r.Context(), req.Title)
	if err != nil {
		h.fail(w, r, err)

		return
	}

	h.write(w, r, http.StatusCreated, toResponse(task))
}

func (h handler) list(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.tasks.List(r.Context())
	if err != nil {
		h.fail(w, r, err)

		return
	}

	body := make([]taskResponse, 0, len(tasks))
	for _, task := range tasks {
		body = append(body, toResponse(task))
	}

	h.write(w, r, http.StatusOK, body)
}

func (h handler) complete(w http.ResponseWriter, r *http.Request) {
	task, err := h.tasks.Complete(r.Context(), domain.ID(r.PathValue("id")))
	if err != nil {
		h.fail(w, r, err)

		return
	}

	h.write(w, r, http.StatusOK, toResponse(task))
}

// fail answers with the status the error maps to; internals stay in the log.
func (h handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	code := statusOf(err)
	message := err.Error()

	if code == http.StatusInternalServerError {
		h.logger.ErrorContext(r.Context(), "request failed", slog.Any("error", err))

		message = http.StatusText(code)
	}

	h.write(w, r, code, errorResponse{Error: message})
}

func (h handler) write(w http.ResponseWriter, r *http.Request, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.logger.ErrorContext(r.Context(), "write response", slog.Any("error", err))
	}
}

func statusOf(err error) int {
	switch {
	case errors.Is(err, errInvalidBody):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrAlreadyDone):
		return http.StatusConflict
	case errors.Is(err, domain.ErrEmptyTitle), errors.Is(err, domain.ErrTitleTooLong):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

func toResponse(task *domain.Task) taskResponse {
	resp := taskResponse{
		ID:        string(task.ID()),
		Title:     task.Title(),
		Done:      task.Done(),
		CreatedAt: task.CreatedAt(),
	}

	if task.Done() {
		completed := task.CompletedAt()
		resp.CompletedAt = &completed
	}

	return resp
}
