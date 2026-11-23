package memory_test

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/latebit-io/go_project/internal/tasks/adapters/memory"
	"github.com/latebit-io/go_project/internal/tasks/domain"
)

func at(hour int) time.Time {
	return time.Date(2026, time.January, 2, hour, 0, 0, 0, time.UTC)
}

func newTask(t *testing.T, id domain.ID, created time.Time) *domain.Task {
	t.Helper()

	task, err := domain.New(id, "task "+string(id), created)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return task
}

func ids(tasks []*domain.Task) string {
	var joined strings.Builder
	for _, task := range tasks {
		joined.WriteString(string(task.ID()))
	}

	return joined.String()
}

func TestListIsOldestFirst(t *testing.T) {
	t.Parallel()

	repo := memory.NewRepository()

	// Saved out of creation order; b and c share an instant and keep their save order.
	saved := []*domain.Task{
		newTask(t, "d", at(12)), newTask(t, "b", at(10)), newTask(t, "c", at(10)), newTask(t, "a", at(9)),
	}
	for _, task := range saved {
		if err := repo.Save(t.Context(), task); err != nil {
			t.Fatalf("Save(%s) error = %v", task.ID(), err)
		}
	}

	// Saving a known task again replaces it and adds nothing.
	if err := repo.Save(t.Context(), newTask(t, "a", at(9))); err != nil {
		t.Fatalf("Save(a) again error = %v", err)
	}

	tasks, err := repo.List(t.Context())
	if err != nil || ids(tasks) != "abcd" {
		t.Fatalf("List() = %s, %v, want abcd", ids(tasks), err)
	}
}

func TestUpdateUnknown(t *testing.T) {
	t.Parallel()

	_, err := memory.NewRepository().Update(t.Context(), "missing", func(*domain.Task) error { return nil })
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Update() error = %v, want %v", err, domain.ErrNotFound)
	}
}

func TestStoredTaskIsIsolatedFromCallers(t *testing.T) {
	t.Parallel()

	repo := memory.NewRepository()
	task := newTask(t, "a", at(9))

	if err := repo.Save(t.Context(), task); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := task.Complete(at(10)); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	stored, err := repo.List(t.Context())
	if err != nil || stored[0].Done() {
		t.Fatalf("List() = %+v, %v, want the stored task still open", stored, err)
	}
}

func TestUpdateKeepsNothingWhenTheChangeFails(t *testing.T) {
	t.Parallel()

	repo := memory.NewRepository()
	if err := repo.Save(t.Context(), newTask(t, "a", at(9))); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	errRefused := errors.New("refused")

	_, err := repo.Update(t.Context(), "a", func(task *domain.Task) error {
		if err := task.Complete(at(10)); err != nil {
			return err
		}

		return errRefused
	})
	if !errors.Is(err, errRefused) {
		t.Fatalf("Update() error = %v, want the change's error", err)
	}

	stored, err := repo.List(t.Context())
	if err != nil || stored[0].Done() {
		t.Fatalf("List() = %+v, %v, want the failed change discarded", stored, err)
	}
}

// Many requests completing one task at once: the domain rule says exactly one wins.
func TestConcurrentUpdatesRunOneAtATime(t *testing.T) {
	t.Parallel()

	repo := memory.NewRepository()
	if err := repo.Save(t.Context(), newTask(t, "a", at(9))); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	var (
		group     sync.WaitGroup
		completed atomic.Int32
	)

	for range 50 {
		group.Go(func() {
			_, err := repo.Update(t.Context(), "a", func(task *domain.Task) error { return task.Complete(at(10)) })
			if err == nil {
				completed.Add(1)
			}
		})
	}

	group.Wait()

	if got := completed.Load(); got != 1 {
		t.Fatalf("%d of 50 concurrent completions succeeded, want exactly 1", got)
	}
}
