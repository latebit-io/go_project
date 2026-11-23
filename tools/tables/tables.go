// Package tables checks that every test function is table-driven: its cases
// are data, run by a loop that calls t.Run for each.
package tables

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
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
		if !isFunc || !isTest(fn) || runsFromTable(fn.Body) {
			continue
		}

		found = append(found, Finding{File: name, Line: fset.Position(fn.Pos()).Line, Test: fn.Name.Name})
	}

	return found, nil
}

// isTest reports a function the go tool runs as a test. TestMain sets up the
// test binary and has no cases.
func isTest(fn *ast.FuncDecl) bool {
	name := fn.Name.Name

	return fn.Recv == nil && fn.Body != nil && strings.HasPrefix(name, "Test") && name != "TestMain"
}

// runsFromTable reports a range loop whose body starts a subtest.
func runsFromTable(body *ast.BlockStmt) bool {
	driven := false

	ast.Inspect(body, func(node ast.Node) bool {
		loop, isRange := node.(*ast.RangeStmt)
		if isRange && startsSubtest(loop.Body) {
			driven = true
		}

		return !driven
	})

	return driven
}

func startsSubtest(body *ast.BlockStmt) bool {
	started := false

	ast.Inspect(body, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return !started
		}

		method, isMethod := call.Fun.(*ast.SelectorExpr)
		if isMethod && method.Sel.Name == "Run" {
			started = true
		}

		return !started
	})

	return started
}
