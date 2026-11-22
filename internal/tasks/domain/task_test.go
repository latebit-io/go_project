package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/latebit-io/go_project/internal/tasks/domain"
)

func at(hour int) time.Time {
	return time.Date(2026, time.January, 2, hour, 0, 0, 0, time.UTC)
}

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		title     string
		wantTitle string
		wantErr   error
	}{
		{name: "trims the title", title: "  write docs ", wantTitle: "write docs"},
		{name: "accepts the longest title", title: strings.Repeat("é", 120), wantTitle: strings.Repeat("é", 120)},
		{name: "rejects a blank title", title: " \t ", wantErr: domain.ErrEmptyTitle},
		{name: "rejects an overlong title", title: strings.Repeat("a", 121), wantErr: domain.ErrTitleTooLong},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			task, err := domain.New("t1", tt.title, at(9))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("New() error = %v, want %v", err, tt.wantErr)
			}

			if tt.wantErr != nil {
				return
			}

			if task.ID() != "t1" || task.Title() != tt.wantTitle || !task.CreatedAt().Equal(at(9)) || task.Done() {
				t.Fatalf("New() = %+v, want open task t1 titled %q created at 9", task, tt.wantTitle)
			}
		})
	}
}

func TestCompleteHappensOnce(t *testing.T) {
	t.Parallel()

	task, err := domain.New("t1", "write docs", at(9))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := task.Complete(at(10)); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if !task.Done() || !task.CompletedAt().Equal(at(10)) {
		t.Fatalf("task = %+v, want done at 10", task)
	}

	if err := task.Complete(at(11)); !errors.Is(err, domain.ErrAlreadyDone) {
		t.Fatalf("second Complete() error = %v, want %v", err, domain.ErrAlreadyDone)
	}

	if !task.CompletedAt().Equal(at(10)) {
		t.Fatalf("CompletedAt() = %v, want the first completion kept", task.CompletedAt())
	}
}
