package coverage_test

import (
	"testing"

	"github.com/latebit-io/go_project/tools/gate/coverage"
)

const exclude = "example.com/shop/cmd/"

func TestPercent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		profile string
		want    float64
		wantOK  bool
	}{
		{
			name: "counts statements outside the excluded path",
			profile: "mode: atomic\n" +
				"example.com/shop/internal/a/a.go:1.1,2.2 3 1\n" +
				"example.com/shop/internal/b/b.go:1.1,2.2 4 0\n" +
				"example.com/shop/internal/b/b.go:3.1,4.2 1 1\n" +
				"example.com/shop/cmd/api/main.go:1.1,9.2 20 0\n",
			want: 50, wantOK: true,
		},
		{
			name: "a block listed twice is covered if any run hit it",
			profile: "mode: atomic\n" +
				"example.com/shop/a.go:1.1,2.2 2 0\n" +
				"example.com/shop/a.go:1.1,2.2 2 3\n",
			want: 100, wantOK: true,
		},
		{
			name:    "lines that are not blocks are ignored",
			profile: "mode: atomic\nnot a block line\nexample.com/shop/a.go:1.1,2.2 x 1\n",
		},
		{name: "empty profile"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := coverage.Percent(tt.profile, exclude)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("Percent() = %v, %v, want %v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
