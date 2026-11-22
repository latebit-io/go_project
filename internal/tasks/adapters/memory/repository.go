// Package memory stores tasks in process memory.
package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/latebit-io/go_project/internal/tasks/domain"
)

var _ domain.Repository = (*Repository)(nil)

// Repository is a concurrency-safe domain.Repository that forgets on exit.
type Repository struct {
	mu    sync.RWMutex
	tasks map[domain.ID]domain.Task
	order []domain.ID
}

// NewRepository returns an empty repository.
func NewRepository() *Repository {
	return &Repository{tasks: map[domain.ID]domain.Task{}}
}

// Save stores a copy, so later changes to task stay outside the repository.
func (r *Repository) Save(_ context.Context, task *domain.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, known := r.tasks[task.ID()]; !known {
		r.order = append(r.order, task.ID())
	}

	r.tasks[task.ID()] = *task

	return nil
}

// Find returns a copy of the task with the ID, or domain.ErrNotFound.
func (r *Repository) Find(_ context.Context, id domain.ID) (*domain.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	task, known := r.tasks[id]
	if !known {
		return nil, fmt.Errorf("find %q: %w", id, domain.ErrNotFound)
	}

	return &task, nil
}

// List returns copies of every task in insertion order.
func (r *Repository) List(context.Context) ([]*domain.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tasks := make([]*domain.Task, 0, len(r.order))
	for _, id := range r.order {
		task := r.tasks[id]
		tasks = append(tasks, &task)
	}

	return tasks, nil
}
