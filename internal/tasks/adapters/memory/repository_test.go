package memory_test

import (
	"errors"
	"testing"
	"time"

	"github.com/latebit-io/go_project/internal/tasks/adapters/memory"
	"github.com/latebit-io/go_project/internal/tasks/domain"
)

func at(hour int) time.Time {
	return time.Date(2026, time.January, 2, hour, 0, 0, 0, time.UTC)
}

func newTask(t *testing.T, id domain.ID) *domain.Task {
	t.Helper()

	task, err := domain.New(id, "task "+string(id), at(9))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return task
}

func TestSaveThenFindAndList(t *testing.T) {
	t.Parallel()

	repo := memory.NewRepository()

	for _, id := range []domain.ID{"b", "a", "b"} {
		if err := repo.Save(t.Context(), newTask(t, id)); err != nil {
			t.Fatalf("Save(%s) error = %v", id, err)
		}
	}

	found, err := repo.Find(t.Context(), "a")
	if err != nil || found.ID() != "a" {
		t.Fatalf("Find(a) = %+v, %v, want task a", found, err)
	}

	tasks, err := repo.List(t.Context())
	if err != nil || len(tasks) != 2 || tasks[0].ID() != "b" || tasks[1].ID() != "a" {
		t.Fatalf("List() = %+v, %v, want b then a", tasks, err)
	}
}

func TestFindUnknown(t *testing.T) {
	t.Parallel()

	if _, err := memory.NewRepository().Find(t.Context(), "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Find() error = %v, want %v", err, domain.ErrNotFound)
	}
}

func TestStoredTaskIsIsolatedFromCallers(t *testing.T) {
	t.Parallel()

	repo := memory.NewRepository()
	task := newTask(t, "a")

	if err := repo.Save(t.Context(), task); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := task.Complete(at(10)); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	stored, err := repo.Find(t.Context(), "a")
	if err != nil || stored.Done() {
		t.Fatalf("Find() = %+v, %v, want the task still open", stored, err)
	}
}
