// Package tables checks that every test function is table-driven: its cases
// are data, run by a loop that calls t.Run for each.
package tables

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Finding is one test function that does not run its cases from a table.
type Finding struct {
	File string
	Line int
	Test string
}

func (f Finding) String() string {
	const advice = "is not table-driven: range over its cases and call t.Run for each"

	return fmt.Sprintf("%s:%d: %s %s", f.File, f.Line, f.Test, advice)
}

// Check returns the test functions of one file that are not table-driven.
// Files that are not Go tests have none.
func Check(name string, src []byte) ([]Finding, error) {
	if !strings.HasSuffix(name, "_test.go") {
		return nil, nil
	}

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}

	var found []Finding

	// The name the file gives the testing package; without the import it
	// cannot declare a test.
	testing := importName(file, "testing")
	if testing == "" {
		return nil, nil
	}

	for _, decl := range file.Decls {
		fn, isFunc := decl.(*ast.FuncDecl)
		if !isFunc || !isTest(fn) || runsFromTable(fn, testing) {
			continue
		}

		found = append(found, Finding{File: name, Line: fset.Position(fn.Pos()).Line, Test: fn.Name.Name})
	}

	return found, nil
}

// isTest reports a function the go tool runs as a test: named Test, or Test
// followed by anything but a lowercase letter. TestMain sets up the test
// binary and has no cases.
func isTest(fn *ast.FuncDecl) bool {
	rest, named := strings.CutPrefix(fn.Name.Name, "Test")
	if !named || fn.Recv != nil || fn.Body == nil || rest == "Main" {
		return false
	}

	first, _ := utf8.DecodeRuneInString(rest)

	return rest == "" || !unicode.IsLower(first)
}

// importName returns the name a file uses for an imported package: its
// alias, "." for a dot import, the default name, or "" when not imported.
func importName(file *ast.File, importPath string) string {
	for _, spec := range file.Imports {
		if strings.Trim(spec.Path.Value, "`\"") != importPath {
			continue
		}

		if spec.Name != nil {
			return spec.Name.Name
		}

		return importPath
	}

	return ""
}

// runsFromTable reports a range loop whose body starts a subtest.
func runsFromTable(fn *ast.FuncDecl, testing string) bool {
	testers := testingParams(fn, testing)
	driven := false

	ast.Inspect(fn.Body, func(node ast.Node) bool {
		loop, isRange := node.(*ast.RangeStmt)
		if isRange && startsSubtest(loop.Body, testers) {
			driven = true
		}

		return !driven
	})

	return driven
}

// testingParams collects the names given to *testing.T parameters, of the
// test itself and of the function literals inside it, whatever they are called.
func testingParams(fn *ast.FuncDecl, testing string) map[string]bool {
	names := map[string]bool{}

	collect := func(params *ast.FieldList) {
		for _, field := range params.List {
			if !isTestingT(field.Type, testing) {
				continue
			}

			for _, name := range field.Names {
				names[name.Name] = true
			}
		}
	}

	collect(fn.Type.Params)

	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if literal, isLiteral := node.(*ast.FuncLit); isLiteral {
			collect(literal.Type.Params)
		}

		return true
	})

	return names
}

// isTestingT reports the type *testing.T, where testing is the name the
// file imports the package under: "." means the type is written *T.
func isTestingT(expr ast.Expr, testing string) bool {
	pointer, isPointer := expr.(*ast.StarExpr)
	if !isPointer {
		return false
	}

	if bare, isIdent := pointer.X.(*ast.Ident); isIdent {
		return testing == "." && bare.Name == "T"
	}

	selector, isSelector := pointer.X.(*ast.SelectorExpr)
	if !isSelector || selector.Sel.Name != "T" {
		return false
	}

	pkg, isIdent := selector.X.(*ast.Ident)

	return isIdent && pkg.Name == testing
}

// startsSubtest reports a call of Run on one of the *testing.T values. Any
// other Run method, such as a command's, is not a subtest.
func startsSubtest(body *ast.BlockStmt, testers map[string]bool) bool {
	started := false

	ast.Inspect(body, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return !started
		}

		method, isMethod := call.Fun.(*ast.SelectorExpr)
		if !isMethod || method.Sel.Name != "Run" {
			return !started
		}

		if receiver, isIdent := method.X.(*ast.Ident); isIdent && testers[receiver.Name] {
			started = true
		}

		return !started
	})

	return started
}
