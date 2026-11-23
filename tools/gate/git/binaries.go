package git

import (
	"context"
	"slices"
	"strings"
)

// StagedBinaries lists compiled executables in the index. It reads the index
// rather than the work tree, so one staged and then deleted is still found.
func (r Repo) StagedBinaries(ctx context.Context) ([]string, error) {
	// mktree with no input names the empty tree; against it, everything staged is new.
	empty, err := r.line(ctx, nil, "mktree")
	if err != nil {
		return nil, err
	}

	stat, err := r.raw(ctx, nil, "diff", "--cached", "--numstat", "-z", empty)
	if err != nil {
		return nil, err
	}

	var found []string

	for record := range strings.SplitSeq(stat, "\x00") {
		// numstat prints "-" for the line counts of a binary blob.
		path, binary := strings.CutPrefix(record, "-\t-\t")
		if !binary {
			continue
		}

		blob, err := r.raw(ctx, nil, "cat-file", "blob", ":"+path)
		if err != nil {
			return nil, err
		}

		if isExecutable(blob) {
			found = append(found, path)
		}
	}

	return found, nil
}

// isExecutable recognizes ELF, Mach-O (thin and fat, both byte orders) and
// Windows PE files by their first bytes.
func isExecutable(blob string) bool {
	magics := []string{
		"\x7fELF", "MZ",
		"\xcf\xfa\xed\xfe", "\xce\xfa\xed\xfe", "\xfe\xed\xfa\xcf", "\xfe\xed\xfa\xce",
		"\xca\xfe\xba\xbe", "\xbe\xba\xfe\xca", "\xca\xfe\xba\xbf", "\xbf\xba\xfe\xca",
	}

	return slices.ContainsFunc(magics, func(magic string) bool { return strings.HasPrefix(blob, magic) })
}
