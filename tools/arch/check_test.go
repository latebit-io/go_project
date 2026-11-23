package arch_test

import (
	"slices"
	"testing"

	"github.com/latebit-io/go_project/tools/arch"
)

const module = "example.com/shop"

const outsideLayout = ": layout: package sits outside cmd/<name>, " +
	"internal/<context>/{domain,app,adapters}, internal/platform, tools"

func pkg(dir, name string, imports ...string) arch.File {
	return arch.File{Dir: dir, Package: name, Imports: imports}
}

func TestCheckAllows(t *testing.T) {
	t.Parallel()

	pkgs := []arch.File{
		pkg("cmd/shop-api", "main", module+"/internal/ordering/adapters/http", module+"/internal/platform/log"),
		pkg("internal/ordering/domain", "domain", "errors", "net/url", module+"/internal/ordering/domain/money"),
		pkg("internal/ordering/domain/money", "money", "math/big"),
		pkg("internal/ordering/app", "app", "context", "log/slog", module+"/internal/ordering/domain"),
		pkg("internal/ordering/adapters/http", "http", "net/http", "github.com/x/router",
			module+"/internal/ordering/app", module+"/internal/platform/log"),
		pkg("internal/platform/log", "log", "log/slog", "github.com/x/y"),
		pkg("tools/gate", "main", "os/exec", module+"/tools/arch"),
	}

	if got := arch.Check(module, pkgs); len(got) != 0 {
		t.Fatalf("Check() = %v, want no violations", got)
	}
}

func TestCheckRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pkg  arch.File
		want string
	}{
		{
			name: "package at module root",
			pkg:  arch.File{Dir: ".", Package: "shop"},
			want: "." + outsideLayout,
		},
		{
			name: "package directly in a context",
			pkg:  pkg("internal/ordering", "ordering"),
			want: "internal/ordering" + outsideLayout,
		},
		{
			name: "nested cmd package",
			pkg:  pkg("cmd/shop/sub", "sub"),
			want: "cmd/shop/sub" + outsideLayout,
		},
		{
			name: "underscore in directory",
			pkg:  pkg("internal/order_mgmt/domain", "domain"),
			want: `internal/order_mgmt/domain: naming: directory "order_mgmt" must be lowercase letters and digits`,
		},
		{
			name: "library under cmd",
			pkg:  pkg("cmd/shop", "shop"),
			want: "cmd/shop: naming: cmd packages must be package main",
		},
		{
			name: "main outside cmd",
			pkg:  pkg("internal/platform/run", "main"),
			want: "internal/platform/run: naming: package main belongs under cmd/<name>",
		},
		{
			name: "package named unlike its directory",
			pkg:  pkg("internal/ordering/domain", "orders"),
			want: `internal/ordering/domain: naming: package "orders" must be named after its directory "domain"`,
		},
		{
			name: "use case links C",
			pkg:  pkg("internal/ordering/app", "app", "C"),
			want: `internal/ordering/app: import: app may not import "C"`,
		},
		{
			name: "adapter links C",
			pkg:  pkg("internal/ordering/adapters/sqlite", "sqlite", "C"),
			want: `internal/ordering/adapters/sqlite: import: adapters may not import "C"`,
		},
		{
			name: "import from a skipped directory",
			pkg:  pkg("internal/ordering/domain", "domain", module+"/internal/ordering/domain/testdata/io"),
			want: `internal/ordering/domain: import: may not import "internal/ordering/domain/testdata/io": ` +
				"the checks skip that directory",
		},
		{
			name: "domain imports app",
			pkg:  pkg("internal/ordering/domain", "domain", module+"/internal/ordering/app"),
			want: `internal/ordering/domain: import: domain may not import app ("internal/ordering/app")`,
		},
		{
			name: "domain imports denied stdlib tree",
			pkg:  pkg("internal/ordering/domain", "domain", "net/http/httptest"),
			want: `internal/ordering/domain: import: domain may not import "net/http/httptest"`,
		},
		{
			name: "domain imports randomness",
			pkg:  pkg("internal/ordering/domain", "domain", "math/rand/v2"),
			want: `internal/ordering/domain: import: domain may not import "math/rand/v2"`,
		},
		{
			name: "domain imports outside module",
			pkg:  pkg("internal/ordering/domain", "domain", "github.com/google/uuid"),
			want: `internal/ordering/domain: import: domain may not import outside modules ("github.com/google/uuid")`,
		},
		{
			name: "app imports adapters",
			pkg:  pkg("internal/ordering/app", "app", module+"/internal/ordering/adapters/sql"),
			want: `internal/ordering/app: import: app may not import adapters ("internal/ordering/adapters/sql")`,
		},
		{
			name: "app imports platform",
			pkg:  pkg("internal/ordering/app", "app", module+"/internal/platform/db"),
			want: `internal/ordering/app: import: app may not import platform ("internal/platform/db")`,
		},
		{
			name: "app imports denied stdlib",
			pkg:  pkg("internal/ordering/app", "app", "database/sql"),
			want: `internal/ordering/app: import: app may not import "database/sql"`,
		},
		{
			name: "context imports another context",
			pkg:  pkg("internal/ordering/adapters/http", "http", module+"/internal/billing/app"),
			want: `internal/ordering/adapters/http: import: may not import another context ` +
				`("internal/billing/app"); wire contexts in cmd`,
		},
		{
			name: "platform imports a context",
			pkg:  pkg("internal/platform/db", "db", module+"/internal/ordering/domain"),
			want: `internal/platform/db: import: platform may not import domain ("internal/ordering/domain")`,
		},
		{
			name: "production code imports tools",
			pkg:  pkg("cmd/shop", "main", module+"/tools/arch"),
			want: `cmd/shop: import: cmd may not import tools ("tools/arch")`,
		},
		{
			name: "tools import outside module",
			pkg:  pkg("tools/gen", "main", "golang.org/x/tools/go/packages"),
			want: `tools/gen: import: tools may not import outside modules ("golang.org/x/tools/go/packages")`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := arch.Check(module, []arch.File{tt.pkg})
			if len(got) != 1 || got[0].String() != tt.want {
				t.Fatalf("Check() = %v, want [%s]", got, tt.want)
			}
		})
	}
}

func TestCheckSortsAndDeduplicates(t *testing.T) {
	t.Parallel()

	pkgs := []arch.File{
		pkg("internal/ordering/domain", "domain", "os", "flag", "os"),
		pkg("internal/billing/domain", "domain", "syscall"),
	}

	want := []string{
		`internal/billing/domain: import: domain may not import "syscall"`,
		`internal/ordering/domain: import: domain may not import "flag"`,
		`internal/ordering/domain: import: domain may not import "os"`,
	}

	got := make([]string, 0, len(want))
	for _, v := range arch.Check(module, pkgs) {
		got = append(got, v.String())
	}

	if !slices.Equal(got, want) {
		t.Fatalf("Check() = %v, want %v", got, want)
	}
}
