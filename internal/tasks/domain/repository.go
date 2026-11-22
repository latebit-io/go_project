package domain

import "context"

// Repository is the port tasks are stored through; adapters implement it.
type Repository interface {
	// Save inserts the task or replaces the one with the same ID.
	Save(ctx context.Context, task *Task) error
	// Find returns the task with the ID, or ErrNotFound.
	Find(ctx context.Context, id ID) (*Task, error)
	// List returns every task, oldest first.
	List(ctx context.Context) ([]*Task, error)
}
