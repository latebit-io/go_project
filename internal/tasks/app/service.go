// Package app holds the task use cases.
package app

import (
	"context"
	"fmt"
	"time"

	"github.com/latebit-io/go_project/internal/tasks/domain"
)

// Service runs the task use cases against its ports.
type Service struct {
	repo  domain.Repository
	now   func() time.Time
	newID func() domain.ID
}

// NewService wires the use cases to a repository, a clock and an ID source.
func NewService(repo domain.Repository, now func() time.Time, newID func() domain.ID) *Service {
	return &Service{repo: repo, now: now, newID: newID}
}

// Add creates and stores an open task.
func (s *Service) Add(ctx context.Context, title string) (*domain.Task, error) {
	task, err := domain.New(s.newID(), title, s.now())
	if err != nil {
		return nil, fmt.Errorf("add task: %w", err)
	}

	if err := s.repo.Save(ctx, task); err != nil {
		return nil, fmt.Errorf("add task: %w", err)
	}

	return task, nil
}

// Complete marks the task with the ID done.
func (s *Service) Complete(ctx context.Context, id domain.ID) (*domain.Task, error) {
	now := s.now()

	// Read, check and write happen inside the repository, so two requests
	// cannot both complete the same task.
	task, err := s.repo.Update(ctx, id, func(task *domain.Task) error { return task.Complete(now) })
	if err != nil {
		return nil, fmt.Errorf("complete task %q: %w", id, err)
	}

	return task, nil
}

// List returns every task, oldest first.
func (s *Service) List(ctx context.Context) ([]*domain.Task, error) {
	tasks, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}

	return tasks, nil
}
