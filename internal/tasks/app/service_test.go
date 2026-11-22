package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/latebit-io/go_project/internal/tasks/app"
	"github.com/latebit-io/go_project/internal/tasks/domain"
)

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

func (f *fakeRepository) Find(_ context.Context, id domain.ID) (*domain.Task, error) {
	task, known := f.tasks[id]
	if !known {
		return nil, domain.ErrNotFound
	}

	return task, nil
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

func TestAdd(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{}

	task, err := newService(repo).Add(t.Context(), "write docs")
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if task.ID() != "t1" || !task.CreatedAt().Equal(now()) || repo.tasks["t1"] != task {
		t.Fatalf("Add() = %+v, want stored task t1 created now", task)
	}
}

func TestAddFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		title   string
		repo    *fakeRepository
		wantErr error
	}{
		{name: "invalid title", title: "", repo: &fakeRepository{}, wantErr: domain.ErrEmptyTitle},
		{name: "storage error", title: "write docs", repo: &fakeRepository{saveErr: errStorage}, wantErr: errStorage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := newService(tt.repo).Add(t.Context(), tt.title); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Add() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestComplete(t *testing.T) {
	t.Parallel()

	service := newService(&fakeRepository{})

	if _, err := service.Add(t.Context(), "write docs"); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	task, err := service.Complete(t.Context(), "t1")
	if err != nil || !task.Done() {
		t.Fatalf("Complete() = %+v, %v, want done task", task, err)
	}

	if _, err := service.Complete(t.Context(), "t1"); !errors.Is(err, domain.ErrAlreadyDone) {
		t.Fatalf("second Complete() error = %v, want %v", err, domain.ErrAlreadyDone)
	}

	if _, err := service.Complete(t.Context(), "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Complete(missing) error = %v, want %v", err, domain.ErrNotFound)
	}
}

func TestCompleteReportsStorageError(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{}
	service := newService(repo)

	if _, err := service.Add(t.Context(), "write docs"); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	repo.saveErr = errStorage

	if _, err := service.Complete(t.Context(), "t1"); !errors.Is(err, errStorage) {
		t.Fatalf("Complete() error = %v, want %v", err, errStorage)
	}
}

func TestList(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{}
	service := newService(repo)

	if _, err := service.Add(t.Context(), "write docs"); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	tasks, err := service.List(t.Context())
	if err != nil || len(tasks) != 1 {
		t.Fatalf("List() = %v, %v, want one task", tasks, err)
	}

	repo.listErr = errStorage

	if _, err := service.List(t.Context()); !errors.Is(err, errStorage) {
		t.Fatalf("List() error = %v, want %v", err, errStorage)
	}
}
