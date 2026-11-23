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

	for _, decl := range file.Decls {
		fn, isFunc := decl.(*ast.FuncDecl)
		if !isFunc || !isTest(fn) || runsFromTable(fn) {
			continue
		}

		found = append(found, Finding{File: name, Line: fset.Position(fn.Pos()).Line, Test: fn.Name.Name})
	}

	return found, nil
}

// isTest reports a function the go tool runs as a test, by the tool's own
// rule: named Test, or Test followed by anything but a lowercase letter, with
// the one parameter of a test. TestMain sets up the binary and has no cases.
func isTest(fn *ast.FuncDecl) bool {
	rest, named := strings.CutPrefix(fn.Name.Name, "Test")
	if !named || fn.Recv != nil || fn.Body == nil || rest == "Main" {
		return false
	}

	first, _ := utf8.DecodeRuneInString(rest)
	if rest != "" && unicode.IsLower(first) {
		return false
	}

	// NumFields is zero for no result list and for an empty one, "()".
	return fn.Type.Results.NumFields() == 0 && takesTestingT(fn.Type.Params)
}

// takesTestingT reports a parameter list of exactly one *testing.T.
func takesTestingT(params *ast.FieldList) bool {
	if len(params.List) != 1 || len(params.List[0].Names) > 1 {
		return false
	}

	return isTestingT(params.List[0].Type)
}

// isTestingT reports a pointer to T or to anything.T. Like the go tool, it
// cannot know how testing was imported or aliased, so the name T decides.
func isTestingT(expr ast.Expr) bool {
	pointer, isPointer := expr.(*ast.StarExpr)
	if !isPointer {
		return false
	}

	if bare, isIdent := pointer.X.(*ast.Ident); isIdent {
		return bare.Name == "T"
	}

	selector, isSelector := pointer.X.(*ast.SelectorExpr)

	return isSelector && selector.Sel.Name == "T"
}

// runsFromTable reports a range loop whose body starts a subtest.
func runsFromTable(fn *ast.FuncDecl) bool {
	testers := testingParams(fn)
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
func testingParams(fn *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}

	collect := func(params *ast.FieldList) {
		for _, field := range params.List {
			if !isTestingT(field.Type) {
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
