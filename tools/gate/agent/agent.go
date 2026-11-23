// Package agent is the step an agent runs when a task is complete: a question
// about a simplify pass after a major Go change, the gate, then a question
// about staging.
package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/latebit-io/go_project/tools/gate/conv"
	"github.com/latebit-io/go_project/tools/gate/git"
)

// majorLines is how many changed Go lines, added plus deleted, make a change major.
const majorLines = 200

// tailLines is how much of a failing gate's output goes back to the agent.
const tailLines = 60

const (
	simplifyQuestion = "Major Go change: %d lines since the last simplify question. Ask the user in your reply " +
		"whether to run a simplify pass on the changes, then stop. Their next message is the answer; " +
		"run the pass only on a yes."
	commitQuestion = "The gate passes and the tree has uncommitted changes. Ask the user in your reply whether " +
		"to stage them with git add -A, and offer a commit subject: conventional type, description of " +
		"%d words at most. Never run git commit; the user commits."
	failedNote  = "The gate failed. Fix the cause; never weaken a guard rail to pass."
	movedNote   = "Files changed while the gate ran, so its pass is not for this tree. Run the step again."
	unchanged   = "The gate failed on this exact tree and nothing changed since; not ready to commit."
	brokenNote  = "The task-complete step could not run: "
	nothingToDo = "ok: nothing uncommitted"
	nothingLeft = "ok: gate passed; nothing to ask"
	// questionPrefix marks output meant for the user rather than a failure to fix.
	questionPrefix = "QUESTION: "
)

// decisionBlock tells Claude Code to keep the agent working and why.
const decisionBlock = "block"

// ErrMode marks a mode the step does not know.
var ErrMode = errors.New("unknown agent mode")

// Config says how the step was started and what it works with.
type Config struct {
	// Mode is "" when an agent or a person runs the step, "stop" or "prompt"
	// from an agent's hooks, and "claude-stop" or "claude-prompt" from Claude Code's.
	Mode string
	Repo git.Repo
	// Tree is the work tree the repository is checked out in.
	Tree fs.FS
	In   io.Reader
	Out  io.Writer
	// Gate runs every check and writes what a person would read.
	Gate func(ctx context.Context, out io.Writer) error
}

// reply is what Claude Code reads from a hook's standard output.
type reply struct {
	Decision      string `json:"decision,omitempty"`
	Reason        string `json:"reason,omitempty"`
	SystemMessage string `json:"systemMessage,omitempty"`
}

// Run performs the step. It reports done when nothing is left for the agent:
// no failing gate and no question to relay. The output says which it is.
func Run(ctx context.Context, cfg *Config) (bool, error) {
	kind, claude := strings.CutPrefix(cfg.Mode, "claude-")
	if (kind != kindStop && kind != kindPrompt) && (kind != kindRun || claude) {
		return false, fmt.Errorf("%w: %q", ErrMode, cfg.Mode)
	}

	done, err := perform(ctx, &session{cfg: cfg, kind: kind, claude: claude})
	if !claude {
		return done, err
	}

	// Claude Code reads JSON and ignores a plain failure exit, so even a step
	// that could not run answers in JSON and reports done.
	if err != nil {
		return true, writeReply(cfg.Out, reply{SystemMessage: brokenNote + err.Error()})
	}

	return true, nil
}

func perform(ctx context.Context, s *session) (bool, error) {
	if err := s.open(ctx); err != nil {
		return false, err
	}

	done, err := s.step(ctx)

	return done, errors.Join(err, s.state.Close())
}

func (s *session) step(ctx context.Context) (bool, error) {
	// Claude Code sends a payload that is not needed, but the pipe must be drained.
	if s.claude {
		if _, err := io.Copy(io.Discard, s.cfg.In); err != nil {
			return false, fmt.Errorf("drain hook input: %w", err)
		}
	}

	// The user has taken their turn. That answers the simplify question either
	// way, and they may have fixed what failed the gate outside the tree.
	if s.kind == kindPrompt {
		return true, errors.Join(s.settleSimplify(), s.forgetFailure())
	}

	dirty, err := s.cfg.Repo.Dirty(ctx)
	if err != nil {
		return false, fmt.Errorf("find changes: %w", err)
	}

	if !dirty {
		return true, s.conclude(nothingToDo)
	}

	if asked, err := s.askAboutSimplify(ctx); asked || err != nil {
		return false, err
	}

	return s.gateThenStaging(ctx)
}

func (s *session) askAboutSimplify(ctx context.Context) (bool, error) {
	goNow, err := s.snapshot(ctx, "*.go")
	if err != nil {
		return false, err
	}

	// Without a prompt hook, work continuing past the question is the only
	// sign that it was answered.
	if s.kind == kindRun && goNow != s.read(fileAsked) {
		err = s.settleSimplify()
		if err != nil {
			return false, err
		}
	}

	// No baseline yet, or git pruned it: count from the last commit.
	base := s.read(fileBaseline)
	if !s.cfg.Repo.HasTree(ctx, base) {
		base = "HEAD"
	}

	lines, err := s.cfg.Repo.ChangedLines(ctx, base, goNow, "*.go")
	if err != nil {
		return false, fmt.Errorf("count changed Go lines: %w", err)
	}

	if lines < majorLines {
		return false, nil
	}

	return s.ask(fileAsked, goNow, fmt.Sprintf(simplifyQuestion, lines))
}

func (s *session) gateThenStaging(ctx context.Context) (bool, error) {
	tree, err := s.identify(ctx)
	if err != nil {
		return false, err
	}

	last := s.read(fileGate)

	// A hook that refused this exact tree before lets it go, or it would loop.
	if s.kind == kindStop && last == tree.key+" fail" {
		if s.claude {
			return true, writeReply(s.cfg.Out, reply{SystemMessage: unchanged})
		}

		return true, s.say(unchanged)
	}

	// Same tree as the last pass: reuse it instead of spending the gate again.
	if !tree.passedAs(last) {
		if passed, err := s.runGate(ctx, tree.key); !passed || err != nil {
			return false, err
		}
	}

	question := fmt.Sprintf(commitQuestion, conv.MaxWords)
	if asked, err := s.ask(fileCommit, tree.work, question); asked || err != nil {
		return false, err
	}

	return true, s.conclude(nothingLeft)
}

// identity names the state the gate judges. key covers what the gate reads
// from git: the work tree, the index, the branch name and the commit.
type identity struct {
	work string
	key  string
	// complete is false when the tree holds a symbolic link: the tests can
	// read through it to content the key does not cover.
	complete bool
}

// passedAs reports whether a remembered result is a pass for this state that
// may be reused. An incomplete key cannot vouch for what the gate would read.
func (t identity) passedAs(last string) bool {
	return t.complete && last == t.key+" pass"
}

func (s *session) identify(ctx context.Context) (identity, error) {
	work, err := s.workTree(ctx)
	if err != nil {
		return identity{}, err
	}

	index, err := s.cfg.Repo.Index(ctx)
	if err != nil {
		return identity{}, fmt.Errorf("read index: %w", err)
	}

	branch, err := s.cfg.Repo.Branch(ctx)
	if err != nil {
		return identity{}, fmt.Errorf("find branch: %w", err)
	}

	head, err := s.cfg.Repo.Head(ctx)
	if err != nil {
		return identity{}, fmt.Errorf("find commit: %w", err)
	}

	linked, err := s.cfg.Repo.HasSymlink(ctx, s.indexPath())
	if err != nil {
		return identity{}, fmt.Errorf("look for symlinks: %w", err)
	}

	sum := sha256.Sum256([]byte(strings.Join([]string{work, index, branch, head}, "\n")))

	return identity{work: work, key: hex.EncodeToString(sum[:]), complete: !linked}, nil
}

func (s *session) runGate(ctx context.Context, key string) (bool, error) {
	var output bytes.Buffer

	gateErr := s.cfg.Gate(ctx, &output)
	if gateErr == nil {
		return s.recordPass(ctx, key)
	}

	if err := s.write(fileGate, key+" fail"); err != nil {
		return false, err
	}

	lines := strings.Split(strings.TrimRight(output.String(), "\n"), "\n")
	tail := strings.Join(lines[max(0, len(lines)-tailLines):], "\n")
	report := fmt.Sprintf("%s\n%v\n%s", tail, gateErr, failedNote)

	// Claude Code sends a blocked stop's reason back to the agent.
	if s.claude {
		return false, writeReply(s.cfg.Out, reply{Decision: decisionBlock, Reason: report})
	}

	return false, s.say(report)
}

// recordPass stores the pass only if the tree is still the one identified
// before the gate ran; the gate reads live files, not a snapshot.
func (s *session) recordPass(ctx context.Context, key string) (bool, error) {
	after, err := s.identify(ctx)
	if err != nil {
		return false, err
	}

	if after.key == key {
		return true, s.write(fileGate, key+" pass")
	}

	if s.claude {
		return false, writeReply(s.cfg.Out, reply{Decision: decisionBlock, Reason: movedNote})
	}

	return false, s.say(movedNote)
}

// ask hands the agent a question to relay to the user, once per tree, and
// reports whether it did.
func (s *session) ask(file, tree, question string) (bool, error) {
	if s.read(file) == tree {
		return false, nil
	}

	if err := s.write(file, tree); err != nil {
		return false, err
	}

	if s.claude {
		return true, writeReply(s.cfg.Out, reply{Decision: decisionBlock, Reason: question, SystemMessage: question})
	}

	return true, s.say(questionPrefix + question)
}

func writeReply(out io.Writer, message reply) error {
	if err := json.NewEncoder(out).Encode(message); err != nil {
		return fmt.Errorf("write hook reply: %w", err)
	}

	return nil
}
