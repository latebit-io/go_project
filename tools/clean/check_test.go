package clean_test

import (
	"strings"
	"testing"

	"github.com/latebit-io/go_project/tools/clean"
)

const (
	narration   = "narration"
	placeholder = "placeholder"
	chat        = "chat"
	changelog   = "changelog"
	emoji       = "emoji"
	constraint  = "constraint"
)

func check(t *testing.T, body string) []clean.Finding {
	t.Helper()

	found, err := clean.NewChecker().Check("x.go", []byte("package x\n\n"+body+"\n"))
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	return found
}

func TestCheckFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		rule string
	}{
		{name: "narrated loop", body: "// Loop through the items", rule: narration},
		{name: "narrated condition", body: "// check if the user exists", rule: narration},
		{name: "narrated construction", body: "// Create a new client", rule: narration},
		{name: "self description", body: "// This function validates input", rule: narration},
		{name: "change log", body: "// Updated to handle nil", rule: changelog},
		{name: "review reference", body: "// renamed per review", rule: changelog},
		{name: "request reference", body: "// split out as requested", rule: changelog},
		{name: "production disclaimer", body: "// in production, you would use a queue", rule: placeholder},
		{name: "admitted stub", body: "// not yet implemented", rule: placeholder},
		{name: "stand-in value", body: "// placeholder value until the API exists", rule: placeholder},
		{name: "elided body", body: "// your logic here", rule: placeholder},
		{name: "first person", body: "// I've kept the old name", rule: chat},
		{name: "pretend code", body: "// In a real implementation this would call the API", rule: placeholder},
		{name: "temporary hack", body: "// hardcoded for now", rule: placeholder},
		{name: "elided code", body: "// ... rest of the function", rule: placeholder},
		{name: "chat opener", body: "// Here's the handler", rule: chat},
		{name: "tutorial voice", body: "// First, we open the file", rule: chat},
		{name: "shouted note", body: "// IMPORTANT: do not remove", rule: chat},
		{name: "plea to the next editor", body: "// Do not remove this check", rule: constraint},
		{name: "passive plea", body: "// the order here must not be changed", rule: constraint},
		{name: "manual coupling", body: "// keep in sync with the client", rule: constraint},
		{name: "filler adverb", body: "// we simply return", rule: "filler"},
		{name: "arrow", body: "// request \u2192 response", rule: "typography"},
		{name: "em dash", body: "// fast \u2014 and wrong", rule: "typography"},
		{name: "block comment", body: "/*\n * Loop over the rows\n */", rule: narration},
		{name: "emoji in comment", body: "// done \u2705", rule: emoji},
		{name: "clock emoji", body: "// soon \u23F0", rule: emoji},
		{name: "emoji in string", body: "const ok = \"\U0001F680 shipped\"", rule: emoji},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			found := check(t, tt.body)
			if len(found) != 1 || found[0].Rule != tt.rule {
				t.Fatalf("Check(%q) = %v, want one %s finding", tt.body, found, tt.rule)
			}
		})
	}
}

func TestCheckAccepts(t *testing.T) {
	t.Parallel()

	bodies := []string{
		"// New returns an open task.\nfunc New() {}",
		"// Updated is emitted when a task changes.\ntype Updated struct{}",
		"// Close the listener first: Shutdown would otherwise wait on it.",
		"// Callers must not modify the returned slice.",
		"// Never change cfg after Start.",
		"// ctx is already canceled; the drain needs its own deadline.",
		"//go:generate stringer -type=Kind",
		"//nolint:gosec // simply a constant",
		"const note = \"Here's a string, not a comment\"",
		"// caf\u00E9 is plain non-ASCII text",
	}

	for _, body := range bodies {
		t.Run(body, func(t *testing.T) {
			t.Parallel()

			if found := check(t, body); len(found) != 0 {
				t.Fatalf("Check(%q) = %v, want no findings", body, found)
			}
		})
	}
}

func TestCheckReportsPosition(t *testing.T) {
	t.Parallel()

	found := check(t, "var a int\n\n/*\n Loop through b\n*/")
	if len(found) != 1 {
		t.Fatalf("Check() = %v, want one finding", found)
	}

	got := found[0].String()
	if want := `x.go:6: narration: say why, not what: "Loop through b"`; got != want {
		t.Fatalf("String() = %s, want %s", got, want)
	}
}

func TestCheckCapsCommentLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "three lines", body: "// a\n// b\n// c"},
		{name: "directives do not count", body: "// a\n// b\n// c\n//nolint:gosec // reason\nvar x int"},
		{name: "separate blocks", body: "// a\n// b\n\n// c\n// d"},
		{name: "slashes inside a string", body: "const s = `\n// a\n// b\n// c\n// d\n`"},
		{
			name: "four lines", body: "// a\n// b\n//\n// d",
			want: `x.go:3: length: move the reasoning to docs/: "4 comment lines, limit 3"`,
		},
		{
			name: "block comment", body: "/*\n a\n b\n*/",
			want: `x.go:3: length: move the reasoning to docs/: "4 comment lines, limit 3"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got string
			if found := check(t, tt.body); len(found) > 0 {
				got = found[0].String()
			}

			if got != tt.want {
				t.Fatalf("Check(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

func TestCheckSkipsGeneratedFiles(t *testing.T) {
	t.Parallel()

	src := "// Code generated by x. DO NOT EDIT.\n\npackage x\n\n// Loop through the items\n"

	found, err := clean.NewChecker().Check("x.go", []byte(src))
	if err != nil || len(found) != 0 {
		t.Fatalf("Check() = %v, %v, want nothing for generated code", found, err)
	}
}

func TestCheckRejectsBrokenSource(t *testing.T) {
	t.Parallel()

	_, err := clean.NewChecker().Check("x.go", []byte("package"))
	if err == nil || !strings.Contains(err.Error(), "parse x.go") {
		t.Fatalf("Check() error = %v, want a parse error naming the file", err)
	}
}
