package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/latebit-io/go_project/internal/tasks/app"
	"github.com/latebit-io/go_project/internal/tasks/domain"
)

const storageFails = "storage error"

var errStorage = errors.New("storage down")

// fakeRepository is the test double for the port; app may not import adapters.
type fakeRepository struct {
	tasks   map[domain.ID]*domain.Task
	saveErr error
	listErr error
}

func (f *fakeRepository) Save(_ context.Context, task *domain.Task) error {
	if f.saveErr != nil {
		return f.saveErr
	}

	f.tasks[task.ID()] = task

	return nil
}

func (f *fakeRepository) Update(
	_ context.Context, id domain.ID, change func(*domain.Task) error,
) (*domain.Task, error) {
	task, known := f.tasks[id]
	if !known {
		return nil, domain.ErrNotFound
	}

	if f.saveErr != nil {
		return nil, f.saveErr
	}

	return task, change(task)
}

func (f *fakeRepository) List(context.Context) ([]*domain.Task, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

	tasks := make([]*domain.Task, 0, len(f.tasks))
	for _, task := range f.tasks {
		tasks = append(tasks, task)
	}

	return tasks, nil
}

func now() time.Time {
	return time.Date(2026, time.January, 2, 9, 0, 0, 0, time.UTC)
}

func newService(repo *fakeRepository) *app.Service {
	repo.tasks = map[domain.ID]*domain.Task{}

	return app.NewService(repo, now, func() domain.ID { return "t1" })
}

// withOpenTask returns a service whose repository holds the open task t1.
func withOpenTask(t *testing.T, repo *fakeRepository) *app.Service {
	t.Helper()

	service := newService(repo)

	if _, err := service.Add(t.Context(), "write docs"); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	return service
}

// withDoneTask returns a service whose repository holds t1, already completed.
func withDoneTask(t *testing.T, repo *fakeRepository) *app.Service {
	t.Helper()

	service := withOpenTask(t, repo)

	if _, err := service.Complete(t.Context(), "t1"); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	return service
}

func TestAdd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		title   string
		repo    *fakeRepository
		wantErr error
	}{
		{name: "stores an open task", title: "write docs", repo: &fakeRepository{}},
		{name: "invalid title", title: "", repo: &fakeRepository{}, wantErr: domain.ErrEmptyTitle},
		{name: storageFails, title: "write docs", repo: &fakeRepository{saveErr: errStorage}, wantErr: errStorage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			task, err := newService(tt.repo).Add(t.Context(), tt.title)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Add() error = %v, want %v", err, tt.wantErr)
			}

			stored := err == nil && task.ID() == "t1" && task.CreatedAt().Equal(now()) && tt.repo.tasks["t1"] == task
			if stored != (tt.wantErr == nil) {
				t.Fatalf("Add() = %+v, stored %v, want stored only on success", task, tt.repo.tasks)
			}
		})
	}
}

func TestComplete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   domain.ID
		// given builds the service and the task it already holds.
		given      func(t *testing.T, repo *fakeRepository) *app.Service
		storageErr error
		wantErr    error
	}{
		{name: "completes an open task", id: "t1", given: withOpenTask},
		{name: "a task completes once", id: "t1", given: withDoneTask, wantErr: domain.ErrAlreadyDone},
		{name: "unknown task", id: "missing", given: withOpenTask, wantErr: domain.ErrNotFound},
		{name: storageFails, id: "t1", given: withOpenTask, storageErr: errStorage, wantErr: errStorage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeRepository{}
			service := tt.given(t, repo)
			repo.saveErr = tt.storageErr

			task, err := service.Complete(t.Context(), tt.id)
			if !errors.Is(err, tt.wantErr) || (err == nil && !task.Done()) {
				t.Fatalf("Complete() = %+v, %v, want error %v", task, err, tt.wantErr)
			}
		})
	}
}

func TestList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		listErr error
		want    int
	}{
		{name: "returns the stored tasks", want: 1},
		{name: storageFails, listErr: errStorage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeRepository{}
			service := withOpenTask(t, repo)
			repo.listErr = tt.listErr

			tasks, err := service.List(t.Context())
			if !errors.Is(err, tt.listErr) || len(tasks) != tt.want {
				t.Fatalf("List() = %v, %v, want %d task(s) and error %v", tasks, err, tt.want, tt.listErr)
			}
		})
	}
}
