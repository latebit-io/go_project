// Package proctest provides a scripted proc.Runner for tests.
package proctest

import (
	"context"
	"slices"
	"strings"
)

// Answer is what a scripted command prints, or the error it fails with.
type Answer struct {
	Out string
	Err error
}

// Fake answers commands from a script instead of starting processes. A key
// in Answers is a command line or a prefix of one; the longest match wins,
// and an unscripted command succeeds with no output.
type Fake struct {
	Answers map[string]Answer

	calls []string
}

// Output implements proc.Runner.
func (f *Fake) Output(_ context.Context, env []string, name string, args ...string) (string, error) {
	answer := f.answer(env, name, args)

	return answer.Out, answer.Err
}

// Stream implements proc.Runner.
func (f *Fake) Stream(_ context.Context, env []string, name string, args ...string) error {
	return f.answer(env, name, args).Err
}

// Calls returns every command so far, environment additions first.
func (f *Fake) Calls() []string {
	return slices.Clone(f.calls)
}

func (f *Fake) answer(env []string, name string, args []string) Answer {
	words := slices.Concat(env, []string{name}, args)
	line := strings.Join(words[len(env):], " ")
	f.calls = append(f.calls, strings.Join(words, " "))

	best := ""

	for key := range f.Answers {
		if strings.HasPrefix(line, key) && len(key) > len(best) {
			best = key
		}
	}

	return f.Answers[best]
}
