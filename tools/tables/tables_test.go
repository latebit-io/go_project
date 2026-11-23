package tables_test

import (
	"cmp"
	"strings"
	"testing"

	"github.com/latebit-io/go_project/tools/tables"
)

const notDriven = "x_test.go:5: TestAdd is not table-driven"

func TestCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file string
		// imports replaces the standard `import "testing"` line.
		imports string
		src     string
		want    string
	}{
		{
			name: "table with subtests",
			src: `func TestAdd(t *testing.T) {
	for _, tt := range []struct{ name string }{{"a"}} {
		t.Run(tt.name, func(t *testing.T) {})
	}
}`,
		},
		{
			name: "map of cases",
			src: `func TestAdd(t *testing.T) {
	for name := range map[string]int{"a": 1} {
		t.Run(name, func(t *testing.T) {})
	}
}`,
		},
		{
			name: "the test parameter under another name",
			src: `func TestAdd(tt *testing.T) {
	for _, name := range []string{"a"} {
		tt.Run(name, func(sub *testing.T) {})
	}
}`,
		},
		{
			name:    "testing imported under another name",
			imports: `import test "testing"`,
			src: `func TestAdd(t *test.T) {
	for _, name := range []string{"a"} {
		t.Run(name, func(t *test.T) {})
	}
}`,
		},
		{
			name:    "testing imported with a dot",
			imports: `import . "testing"`,
			src: `func TestAdd(t *T) {
	for _, name := range []string{"a"} {
		t.Run(name, func(t *T) {})
	}
}`,
		},
		{
			name:    "an aliased test without a table is still found",
			imports: `import test "testing"`,
			src:     "func TestAdd(t *test.T) {}",
			want:    notDriven,
		},
		{
			name: "a loop that runs something else, not a subtest",
			src: `func TestAdd(t *testing.T) {
	for _, cmd := range []runner{} {
		cmd.Run()
		start().Run()
	}
}`,
			want: notDriven,
		},
		{
			name: "a name the go tool does not run as a test",
			src:  "func Testhelper(t *testing.T) {\n\tt.Helper()\n}",
		},
		{
			name: "the bare name Test is a test",
			src:  "func Test(t *testing.T) {}",
			want: "x_test.go:5: Test is not table-driven",
		},
		{
			// The go tool only runs Test functions with the test signature.
			name:    "a Test name without the test signature is a helper",
			imports: `import "fmt"`,
			src:     "func TestAdd() {\n\tfmt.Println()\n}\n\nfunc TestSum(n *int) {}\n\nfunc TestMax(t *T) int { return 0 }",
		},
		{
			// type T = testing.T, declared in another file of the package.
			name:    "a test through a type alias from another file",
			imports: "var other = 0",
			src:     "func TestAdd(t *T) {}",
			want:    notDriven,
		},
		{
			name:    "a table test through a type alias from another file",
			imports: "var other = 0",
			src: `func TestAdd(t *T) {
	for _, name := range []string{"a"} {
		t.Run(name, func(t *T) {})
	}
}`,
		},
		{
			name: "single scenario",
			src:  "func TestAdd(t *testing.T) {\n\tif 1+1 != 2 {\n\t\tt.Fatal(\"math\")\n\t}\n}",
			want: notDriven,
		},
		{
			name: "loop without subtests",
			src:  "func TestAdd(t *testing.T) {\n\tfor _, n := range []int{1} {\n\t\t_ = n\n\t}\n}",
			want: notDriven,
		},
		{
			name: "subtest without a loop",
			src:  "func TestAdd(t *testing.T) {\n\tt.Run(\"a\", func(t *testing.T) {})\n}",
			want: notDriven,
		},
		{
			name: "TestMain has no cases",
			src:  "func TestMain(m *testing.M) {\n\tm.Run()\n}",
		},
		{
			name: "helpers and methods are not tests",
			src:  "type suite struct{}\n\nfunc (suite) TestAdd(t *testing.T) {}\n\nfunc helper(t *testing.T) {}",
		},
		{
			name: "production file",
			file: "x.go",
			src:  "func TestAdd(t *testing.T) {}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file := cmp.Or(tt.file, "x_test.go")
			imports := cmp.Or(tt.imports, `import "testing"`)

			found, err := tables.Check(file, []byte("package x\n\n"+imports+"\n\n"+tt.src+"\n"))
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}

			var got string
			if len(found) > 0 {
				got = found[0].String()
			}

			if !strings.HasPrefix(got, tt.want) || (tt.want == "" && got != "") {
				t.Fatalf("Check() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckRejectsBrokenSource(t *testing.T) {
	t.Parallel()

	for _, src := range []string{"package", "package x\n\nfunc TestAdd("} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()

			if _, err := tables.Check("x_test.go", []byte(src)); err == nil {
				t.Fatal("Check() error = nil, want a parse error")
			}
		})
	}
}
