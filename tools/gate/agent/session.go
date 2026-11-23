package agent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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
	fileTracked  = "tracked"

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

// forgetFailure drops a remembered gate failure so the next stop runs the
// gate again. A remembered pass stays: it can only go stale by a tree change.
func (s *session) forgetFailure() error {
	if !strings.HasSuffix(s.read(fileGate), " fail") {
		return nil
	}

	if err := s.state.Remove(fileGate); err != nil {
		return fmt.Errorf("forget gate failure: %w", err)
	}

	return nil
}

// scratchIndex returns the path of an index file that does not exist yet, so
// a snapshot keeps nothing from the one before.
func (s *session) scratchIndex() (string, error) {
	if err := s.state.Remove(fileIndex); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("reset scratch index: %w", err)
	}

	return filepath.Join(s.state.Name(), fileIndex), nil
}

// snapshot stores a tree of the non-ignored files under the pathspec, tracked or not.
func (s *session) snapshot(ctx context.Context, pathspec string) (string, error) {
	index, err := s.scratchIndex()
	if err != nil {
		return "", err
	}

	tree, err := s.cfg.Repo.Snapshot(ctx, index, pathspec)
	if err != nil {
		return "", fmt.Errorf("snapshot %s: %w", pathspec, err)
	}

	return tree, nil
}

// workTree stores a tree of everything the gate can read from the work tree:
// every tracked file as it is on disk, plus untracked files that are not ignored.
func (s *session) workTree(ctx context.Context) (string, error) {
	index, err := s.scratchIndex()
	if err != nil {
		return "", err
	}

	err = s.addTracked(ctx, index)
	if err != nil {
		return "", err
	}

	tree, err := s.cfg.Repo.Snapshot(ctx, index, ".")
	if err != nil {
		return "", fmt.Errorf("snapshot work tree: %w", err)
	}

	return tree, nil
}

// addTracked puts every tracked file into the scratch index by name. A plain
// git add skips a tracked file that .gitignore matches, and would miss edits to it.
func (s *session) addTracked(ctx context.Context, index string) error {
	tracked, err := s.cfg.Repo.Tracked(ctx)
	if err != nil {
		return fmt.Errorf("list tracked files: %w", err)
	}

	// A tracked file deleted on disk has nothing to add, and git would refuse
	// the list. Lstat, so a symlink counts even when its target is missing.
	present := slices.DeleteFunc(tracked, func(name string) bool {
		_, statErr := fs.Lstat(s.cfg.Tree, name)

		return statErr != nil
	})
	if len(present) == 0 {
		return nil
	}

	err = s.state.WriteFile(fileTracked, []byte(strings.Join(present, "\x00")), fileMode)
	if err != nil {
		return fmt.Errorf("write tracked list: %w", err)
	}

	err = s.cfg.Repo.Force(ctx, index, filepath.Join(s.state.Name(), fileTracked))
	if err != nil {
		return fmt.Errorf("add tracked files: %w", err)
	}

	return nil
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
