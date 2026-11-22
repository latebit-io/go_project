// Package arch holds the layout, naming and import rules of this module.
package arch

type layer string

const (
	layerDomain   layer = "domain"
	layerApp      layer = "app"
	layerAdapters layer = "adapters"
	layerPlatform layer = "platform"
	layerCmd      layer = "cmd"
	layerTooling  layer = "tools"
)

const layoutDetail = "package sits outside cmd/<name>, " +
	"internal/<context>/{domain,app,adapters}, internal/platform, tools"

// Platforms lists what the module supports. Every file must build on all of
// them: the gate cross-builds each, and lint and tests see the same code everywhere.
func Platforms() []string {
	return []string{"linux/amd64", "darwin/arm64", "windows/amd64"}
}

// mainRule says whether a layer's packages are commands.
type mainRule int

const (
	mainForbidden mainRule = iota
	mainAllowed
	mainRequired
)

// policy is everything one layer may do; check.go only applies it.
type policy struct {
	sameContext []layer  // layers of the importer's own bounded context
	anyContext  []layer  // layers importable from anywhere
	stdlibDeny  []string // "p" is exact, "p/..." covers p and below
	external    bool     // may link modules outside this one
	scoped      bool     // lives at internal/<context>/<layer>
	dirExtra    string   // directory name characters allowed beyond a-z, 0-9
	main        mainRule
}

func policyOf(l layer) policy {
	switch l {
	case layerDomain:
		return policy{scoped: true, sameContext: []layer{layerDomain}, stdlibDeny: domainDeny()}
	case layerApp:
		return policy{scoped: true, sameContext: []layer{layerDomain, layerApp}, stdlibDeny: appDeny()}
	case layerAdapters:
		return policy{
			scoped:      true,
			sameContext: []layer{layerDomain, layerApp, layerAdapters},
			anyContext:  []layer{layerPlatform},
			external:    true,
		}
	case layerPlatform:
		return policy{anyContext: []layer{layerPlatform}, external: true}
	case layerCmd:
		return policy{
			anyContext: []layer{layerDomain, layerApp, layerAdapters, layerPlatform},
			external:   true,
			dirExtra:   "-",
			main:       mainRequired,
		}
	case layerTooling:
		return policy{anyContext: []layer{layerTooling}, main: mainAllowed}
	default:
		return policy{}
	}
}

// appDeny keeps I/O and delivery mechanisms out of use cases.
func appDeny() []string {
	return []string{
		"C", "database/...", "embed", "flag", "html/...", "log", "net", "net/http/...",
		"net/rpc/...", "net/smtp", "os/...", "plugin", "syscall", "text/template/...", "unsafe",
	}
}

// domainDeny adds serialization, logging, randomness and reflection to appDeny.
func domainDeny() []string {
	return append(appDeny(),
		"crypto/rand", "encoding/csv", "encoding/gob", "encoding/json", "encoding/xml",
		"io/fs", "io/ioutil", "log/...", "math/rand/...", "reflect", "runtime/...",
	)
}
