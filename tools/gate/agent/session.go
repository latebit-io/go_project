package agent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The kinds of run: by hand or by an agent, from a stop hook, from a prompt hook.
const (
	kindRun    = ""
	kindStop   = "stop"
	kindPrompt = "prompt"
)

// State files, kept in the git directory so they never show up as changes.
const (
	stateDir     = "agent-gate"
	fileAsked    = "simplify-asked"
	fileBaseline = "simplify-baseline"
	fileGate     = "gate-result"
	fileCommit   = "commit-asked"
	fileIndex    = "index"

	dirMode  = 0o750
	fileMode = 0o600
)

// session is one run of the step.
type session struct {
	cfg    *Config
	kind   string
	claude bool
	state  *os.Root
}

func (s *session) open(ctx context.Context) error {
	gitDir, err := s.cfg.Repo.Dir(ctx)
	if err != nil {
		return fmt.Errorf("find git directory: %w", err)
	}

	dir := filepath.Join(gitDir, stateDir)

	err = os.MkdirAll(dir, dirMode)
	if err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	s.state, err = os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open state directory: %w", err)
	}

	return nil
}

// read returns a state value, or "" when it was never written.
func (s *session) read(name string) string {
	data, err := s.state.ReadFile(name)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

func (s *session) write(name, value string) error {
	if err := s.state.WriteFile(name, []byte(value+"\n"), fileMode); err != nil {
		return fmt.Errorf("write state %s: %w", name, err)
	}

	return nil
}

// settleSimplify records that the simplify question was answered: later Go
// changes are counted from the code it was asked about.
func (s *session) settleSimplify() error {
	err := s.state.Rename(fileAsked, fileBaseline)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("settle simplify question: %w", err)
	}

	return nil
}

// snapshot stores a tree of the non-ignored files under the pathspec, tracked or not.
func (s *session) snapshot(ctx context.Context, pathspec string) (string, error) {
	// The scratch index must start empty, or the tree keeps files from the last snapshot.
	if err := s.state.Remove(fileIndex); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("reset scratch index: %w", err)
	}

	tree, err := s.cfg.Repo.Snapshot(ctx, filepath.Join(s.state.Name(), fileIndex), pathspec)
	if err != nil {
		return "", fmt.Errorf("snapshot %s: %w", pathspec, err)
	}

	return tree, nil
}

// say writes one line for the agent or the person running the step.
func (s *session) say(text string) error {
	if _, err := fmt.Fprintln(s.cfg.Out, text); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	return nil
}

// conclude reports a quiet outcome, which only a direct run prints: a hook
// stays silent when there is nothing to do.
func (s *session) conclude(message string) error {
	if s.kind != kindRun {
		return nil
	}

	return s.say(message)
}
