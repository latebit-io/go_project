package arch

import (
	"path"
	"slices"
	"strings"

	"github.com/latebit-io/go_project/tools/srcwalk"
)

const (
	ruleLayout = "layout"
	ruleNaming = "naming"
	ruleImport = "import"
	ruleBuild  = "build"
)

// File is what the rules need from one Go file. Dir is relative to the module
// root; Package has any _test suffix removed.
type File struct {
	Dir      string
	Name     string
	Package  string
	Imports  []string
	Excluded string // why some build leaves the file out, or ""
}

// Violation is one broken rule in one package.
type Violation struct {
	Package string
	Rule    string
	Detail  string
}

func (v Violation) String() string {
	return v.Package + ": " + v.Rule + ": " + v.Detail
}

// place is where a package sits in the layout.
type place struct {
	layer   layer
	context string
}

// Check reports every layout, naming and import violation in files, sorted,
// each once however many files of a package share it.
func Check(module string, files []File) []Violation {
	found := make([]Violation, 0, len(files))
	for i := range files {
		found = append(found, checkFile(module, &files[i])...)
	}

	slices.SortFunc(found, func(a, b Violation) int {
		return strings.Compare(a.String(), b.String())
	})

	return slices.Compact(found)
}

func checkFile(module string, file *File) []Violation {
	rel := file.Dir

	from, ok := locate(rel)
	if !ok {
		return []Violation{{Package: rel, Rule: ruleLayout, Detail: layoutDetail}}
	}

	var found []Violation

	if file.Excluded != "" {
		detail := file.Name + " " + file.Excluded + ", so lint and tests would miss it somewhere"
		found = append(found, Violation{Package: rel, Rule: ruleBuild, Detail: detail})
	}

	for _, detail := range namingDetails(rel, from, file.Package) {
		found = append(found, Violation{Package: rel, Rule: ruleNaming, Detail: detail})
	}

	for _, imp := range file.Imports {
		if detail := importDetail(module, from, imp); detail != "" {
			found = append(found, Violation{Package: rel, Rule: ruleImport, Detail: detail})
		}
	}

	return found
}

// relative strips the module prefix; the module root itself becomes ".".
func relative(module, importPath string) string {
	if importPath == module {
		return "."
	}

	return strings.TrimPrefix(importPath, module+"/")
}

func locate(rel string) (place, bool) {
	root, rest, _ := strings.Cut(rel, "/")

	switch root {
	case string(layerTooling):
		return place{layer: layerTooling}, true
	case string(layerCmd):
		isCommand := rest != "" && !strings.Contains(rest, "/")

		return place{layer: layerCmd}, isCommand
	case "internal":
		return locateInternal(rest)
	default:
		return place{}, false
	}
}

// locateInternal maps a path below internal/ to platform or a context layer.
func locateInternal(rest string) (place, bool) {
	context, sub, _ := strings.Cut(rest, "/")
	if context == string(layerPlatform) {
		return place{layer: layerPlatform}, true
	}

	name, _, _ := strings.Cut(sub, "/")
	at := place{layer: layer(name), context: context}

	return at, policyOf(at.layer).scoped
}

func namingDetails(rel string, at place, pkgName string) []string {
	var details []string

	rules := policyOf(at.layer)

	for elem := range strings.SplitSeq(rel, "/") {
		if !validName(elem, rules.dirExtra) {
			details = append(details, `directory "`+elem+`" must be lowercase letters and digits`)
		}
	}

	if detail := packageDetail(at, pkgName, path.Base(rel)); detail != "" {
		details = append(details, detail)
	}

	return details
}

// packageDetail checks the package clause against the directory and the
// layer's rule for commands.
func packageDetail(at place, pkgName, dirName string) string {
	rule := policyOf(at.layer).main
	isMain := pkgName == "main"

	switch {
	case rule == mainRequired && !isMain:
		return string(at.layer) + " packages must be package main"
	case rule == mainForbidden && isMain:
		return "package main belongs under cmd/<name>"
	case !isMain && pkgName != dirName:
		return `package "` + pkgName + `" must be named after its directory "` + dirName + `"`
	default:
		return ""
	}
}

// validName accepts a lowercase letter followed by lowercase letters, digits and extra.
func validName(name, extra string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}

	for _, r := range name {
		lower := r >= 'a' && r <= 'z'
		digit := r >= '0' && r <= '9'

		if !lower && !digit && !strings.ContainsRune(extra, r) {
			return false
		}
	}

	return true
}

// importDetail returns why from may not import imp, or "" when it may.
func importDetail(module string, from place, imp string) string {
	if rel := relative(module, imp); rel != imp {
		return internalDetail(from, rel)
	}

	rules := policyOf(from.layer)
	denied := func(pattern string) bool { return matches(pattern, imp) }

	switch {
	case isStdlib(imp) && slices.ContainsFunc(rules.stdlibDeny, denied):
		return string(from.layer) + ` may not import "` + imp + `"`
	case !isStdlib(imp) && !rules.external:
		return string(from.layer) + ` may not import outside modules ("` + imp + `")`
	default:
		return ""
	}
}

func internalDetail(from place, rel string) string {
	// No check reads these directories, so code there must stay unreachable.
	if slices.ContainsFunc(strings.Split(rel, "/"), srcwalk.Skips) {
		return `may not import "` + rel + `": the checks skip that directory`
	}

	rules := policyOf(from.layer)

	to, ok := locate(rel)
	if !ok || slices.Contains(rules.anyContext, to.layer) {
		return "" // an unmapped target is reported as its own layout violation
	}

	if !slices.Contains(rules.sameContext, to.layer) {
		return string(from.layer) + " may not import " + string(to.layer) + ` ("` + rel + `")`
	}

	if to.context != from.context {
		return `may not import another context ("` + rel + `"); wire contexts in cmd`
	}

	return ""
}

func isStdlib(imp string) bool {
	first, _, _ := strings.Cut(imp, "/")

	return !strings.Contains(first, ".")
}

func matches(pattern, imp string) bool {
	prefix, tree := strings.CutSuffix(pattern, "/...")
	if !tree {
		return imp == pattern
	}

	return imp == prefix || strings.HasPrefix(imp, prefix+"/")
}
