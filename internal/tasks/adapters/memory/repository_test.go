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

var errRefused = errors.New("refused")

func at(hour int) time.Time {
	return time.Date(2026, time.January, 2, hour, 0, 0, 0, time.UTC)
}

// saved is a task to store: its ID and the hour it was created.
type saved struct {
	id   domain.ID
	hour int
}

// repository returns a repository holding the tasks, saved in the given order.
func repository(t *testing.T, tasks ...saved) *memory.Repository {
	t.Helper()

	repo := memory.NewRepository()

	for _, each := range tasks {
		task, err := domain.New(each.id, "task "+string(each.id), at(each.hour))
		if err != nil {
			t.Fatalf("New(%s) error = %v", each.id, err)
		}

		if err := repo.Save(t.Context(), task); err != nil {
			t.Fatalf("Save(%s) error = %v", each.id, err)
		}
	}

	return repo
}

func complete(task *domain.Task) error { return task.Complete(at(10)) }

func TestList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		saves []saved
		want  string
	}{
		{name: "empty"},
		{name: "oldest first, whatever the save order", saves: []saved{{"c", 12}, {"a", 9}, {"b", 10}}, want: "abc"},
		{name: "the same instant keeps the save order", saves: []saved{{"y", 10}, {"x", 10}}, want: "yx"},
		{name: "saving again replaces and adds nothing", saves: []saved{{"a", 9}, {"b", 10}, {"a", 9}}, want: "ab"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tasks, err := repository(t, tt.saves...).List(t.Context())
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}

			var got strings.Builder
			for _, task := range tasks {
				got.WriteString(string(task.ID()))
			}

			if got.String() != tt.want {
				t.Fatalf("List() = %s, want %s", got.String(), tt.want)
			}
		})
	}
}

func TestUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		id       domain.ID
		change   func(*domain.Task) error
		wantErr  error
		wantDone bool
	}{
		{name: "stores the change", id: "a", change: complete, wantDone: true},
		{name: "unknown task", id: "missing", change: complete, wantErr: domain.ErrNotFound},
		{
			name: "a failed change is discarded", id: "a", wantErr: errRefused,
			change: func(task *domain.Task) error { return errors.Join(complete(task), errRefused) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := repository(t, saved{"a", 9})

			if _, err := repo.Update(t.Context(), tt.id, tt.change); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Update() error = %v, want %v", err, tt.wantErr)
			}

			stored, err := repo.List(t.Context())
			if err != nil || stored[0].Done() != tt.wantDone {
				t.Fatalf("stored task = %+v, %v, want done = %v", stored, err, tt.wantDone)
			}
		})
	}
}

// What a caller holds is a copy: changing it must not reach the stored task.
func TestStoredTaskIsIsolatedFromCallers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// held returns a task the caller got from, or gave to, the repository.
		held func(t *testing.T, repo *memory.Repository) *domain.Task
	}{
		{
			name: "a task read back by List",
			held: func(t *testing.T, repo *memory.Repository) *domain.Task {
				t.Helper()

				tasks, err := repo.List(t.Context())
				if err != nil {
					t.Fatalf("List() error = %v", err)
				}

				return tasks[0]
			},
		},
		{
			name: "a task returned by Update",
			held: func(t *testing.T, repo *memory.Repository) *domain.Task {
				t.Helper()

				task, err := repo.Update(t.Context(), "a", func(*domain.Task) error { return nil })
				if err != nil {
					t.Fatalf("Update() error = %v", err)
				}

				return task
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := repository(t, saved{"a", 9})

			if err := complete(tt.held(t, repo)); err != nil {
				t.Fatalf("Complete() error = %v", err)
			}

			stored, err := repo.List(t.Context())
			if err != nil || stored[0].Done() {
				t.Fatalf("stored task = %+v, %v, want it still open", stored, err)
			}
		})
	}
}

// Many requests completing one task at once: the domain rule says exactly one wins.
func TestConcurrentUpdatesRunOneAtATime(t *testing.T) {
	t.Parallel()

	for _, workers := range []int{2, 50} {
		t.Run(time.Duration(workers).String(), func(t *testing.T) {
			t.Parallel()

			repo := repository(t, saved{"a", 9})

			var (
				group     sync.WaitGroup
				completed atomic.Int32
			)

			for range workers {
				group.Go(func() {
					if _, err := repo.Update(t.Context(), "a", complete); err == nil {
						completed.Add(1)
					}
				})
			}

			group.Wait()

			if got := completed.Load(); got != 1 {
				t.Fatalf("%d of %d concurrent completions succeeded, want exactly 1", got, workers)
			}
		})
	}
}
