package domain

import "context"

// Repository is the port tasks are stored through; adapters implement it.
type Repository interface {
	// Save inserts the task or replaces the one with the same ID.
	Save(ctx context.Context, task *Task) error
	// Update applies change to the stored task and saves the result as one
	// atomic step. It fails with ErrNotFound, or with what change returns.
	Update(ctx context.Context, id ID, change func(*Task) error) (*Task, error)
	// List returns every task, oldest first.
	List(ctx context.Context) ([]*Task, error)
}
