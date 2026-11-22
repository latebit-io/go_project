// Package domain models tasks and the rules that keep them valid.
package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const maxTitleRunes = 120

var (
	// ErrEmptyTitle rejects a task whose title is blank.
	ErrEmptyTitle = errors.New("task title is empty")
	// ErrTitleTooLong rejects a title above 120 characters.
	ErrTitleTooLong = errors.New("task title is longer than 120 characters")
	// ErrAlreadyDone rejects completing a task twice.
	ErrAlreadyDone = errors.New("task is already done")
	// ErrNotFound reports that no task has the requested ID.
	ErrNotFound = errors.New("task not found")
)

// ID identifies a task.
type ID string

// Task is a unit of work: open when created, done once completed.
type Task struct {
	id          ID
	title       string
	createdAt   time.Time
	completedAt time.Time // zero while open
}

// New returns an open task. Identity and time come from the caller, which
// keeps the domain deterministic.
func New(id ID, title string, now time.Time) (*Task, error) {
	trimmed := strings.TrimSpace(title)

	switch {
	case trimmed == "":
		return nil, ErrEmptyTitle
	case utf8.RuneCountInString(trimmed) > maxTitleRunes:
		return nil, ErrTitleTooLong
	default:
		return &Task{id: id, title: trimmed, createdAt: now}, nil
	}
}

// ID returns the task's identity.
func (t *Task) ID() ID { return t.id }

// Title returns the trimmed title.
func (t *Task) Title() string { return t.title }

// CreatedAt returns when the task was added.
func (t *Task) CreatedAt() time.Time { return t.createdAt }

// CompletedAt returns when the task was completed; zero while open.
func (t *Task) CompletedAt() time.Time { return t.completedAt }

// Done reports whether the task has been completed.
func (t *Task) Done() bool { return !t.completedAt.IsZero() }

// Complete marks the task done at now. A task completes once.
func (t *Task) Complete(now time.Time) error {
	if t.Done() {
		return ErrAlreadyDone
	}

	t.completedAt = now

	return nil
}
