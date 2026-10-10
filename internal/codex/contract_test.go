package codex

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"aacpanel/internal/codex/contract"
)

// The contract of the protocol is checked against a new release of codex
// only for what it names, so it has to name every method the link uses: a
// call added to the code and left out of uses.txt is one a release can take
// away unnoticed. The methods are read off the source — every literal the
// link calls, notifies or compares a notification with — and the lists the
// link keeps.
func TestTheContractNamesEveryMethodOfTheLink(t *testing.T) {
	c, err := contract.Panel()
	if err != nil {
		t.Fatal(err)
	}
	calls, tells, hears := methodsOfTheSource(t)
	same := func(what string, code, written []string) {
		t.Helper()
		code, written = slices.Sorted(slices.Values(code)), slices.Sorted(slices.Values(written))
		code, written = slices.Compact(code), slices.Compact(written)
		for _, m := range code {
			if !slices.Contains(written, m) {
				t.Errorf("the link uses %s %s, and uses.txt of the contract does not name it", what, m)
			}
		}
		for _, m := range written {
			if !slices.Contains(code, m) {
				t.Errorf("uses.txt names %s %s, and the link does not use it", what, m)
			}
		}
	}
	same("the request", calls, c.Methods(contract.ClientRequest))
	same("the notification it sends", tells, c.Methods(contract.ClientNotification))
	same("the notification it reads", hears, c.Methods(contract.ServerNotification))
	same("the request it answers", waitsForPerson, c.Methods(contract.ServerRequest))
	same("the notification it turns off", quiet, c.Quiet)
}

// methodsOfTheSource reads the methods out of the files of the package: the
// literal a call goes out with, through within or the connection itself, and
// the literal a notification is told by in handle.
func methodsOfTheSource(t *testing.T) (calls, tells, hears []string) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				var arg ast.Expr
				switch fn := x.Fun.(type) {
				case *ast.Ident:
					if fn.Name == "within" && len(x.Args) > 2 {
						arg = x.Args[2]
					}
				case *ast.SelectorExpr:
					if (fn.Sel.Name == "call" || fn.Sel.Name == "notify") && len(x.Args) > 1 {
						arg = x.Args[1]
					}
				}
				if arg == nil {
					return true
				}
				lit, ok := arg.(*ast.BasicLit)
				if !ok {
					// The method within and call pass on is a parameter.
					if id, ok := arg.(*ast.Ident); ok && id.Name == "method" {
						return true
					}
					t.Errorf("%s: a method the test cannot read — call it with a literal", fset.Position(arg.Pos()))
					return true
				}
				m, _ := strconv.Unquote(lit.Value)
				if fn, ok := x.Fun.(*ast.SelectorExpr); ok && fn.Sel.Name == "notify" {
					tells = append(tells, m)
				} else {
					calls = append(calls, m)
				}
			case *ast.BinaryExpr:
				sel, ok := x.X.(*ast.SelectorExpr)
				lit, isLit := x.Y.(*ast.BasicLit)
				if x.Op == token.EQL && ok && isLit && sel.Sel.Name == "Method" {
					m, _ := strconv.Unquote(lit.Value)
					hears = append(hears, m)
				}
			}
			return true
		})
	}
	if len(calls) == 0 || len(hears) == 0 {
		t.Fatal("no method was read off the source: the walk looks for what the link no longer does")
	}
	return calls, tells, hears
}
