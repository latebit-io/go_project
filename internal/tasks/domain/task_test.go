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

func TestComplete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// hours at which Complete is called, in order
		completions []int
		// error of the last call
		wantErr    error
		wantDoneAt int
	}{
		{name: "an open task completes", completions: []int{10}, wantDoneAt: 10},
		{
			name: "a task completes once and keeps the first time", completions: []int{10, 11},
			wantErr: domain.ErrAlreadyDone, wantDoneAt: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			task, err := domain.New("t1", "write docs", at(9))
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			for _, hour := range tt.completions {
				err = task.Complete(at(hour))
			}

			if !errors.Is(err, tt.wantErr) || !task.Done() || !task.CompletedAt().Equal(at(tt.wantDoneAt)) {
				t.Fatalf("after %v: error = %v, task = %+v, want %+v", tt.completions, err, task, tt)
			}
		})
	}
}
