package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestBuildAPIRoutes_ComposesEveryFeatureRoutesFunc fails when a top-level
// func returning []apiRoute and named xxxRoutes exists but buildAPIRoutes never
// calls it, which would leave that feature's endpoints silently unregistered
// (docs/adr/0177).
func TestBuildAPIRoutes_ComposesEveryFeatureRoutesFunc(t *testing.T) {
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	called := map[string]bool{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Type.Results == nil || len(fd.Type.Results.List) != 1 {
				continue
			}
			if arr, ok := fd.Type.Results.List[0].Type.(*ast.ArrayType); ok {
				if id, ok := arr.Elt.(*ast.Ident); ok && id.Name == "apiRoute" && strings.HasSuffix(fd.Name.Name, "Routes") && fd.Name.Name != "buildAPIRoutes" {
					declared[fd.Name.Name] = true
				}
			}
			if fd.Name.Name == "buildAPIRoutes" {
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					if call, ok := n.(*ast.CallExpr); ok {
						if id, ok := call.Fun.(*ast.Ident); ok {
							called[id.Name] = true
						}
					}
					return true
				})
			}
		}
	}
	var missing []string
	for name := range declared {
		if !called[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(declared) == 0 {
		t.Fatal("found no xxxRoutes functions; the scan is broken")
	}
	if len(missing) > 0 {
		t.Fatalf("routes functions never composed by buildAPIRoutes: %v", missing)
	}
}

// TestBuildAPIRoutes_AdminPrefixesAreAdminTier pins the admin surfaces from
// ADR 0040's tier table (the "admin" row: /api/settings/*, /api/plugins/*,
// /api/alarm-transports*). A feature's route function can set any other tier
// freely, but these prefixes must stay admin.
func TestBuildAPIRoutes_AdminPrefixesAreAdminTier(t *testing.T) {
	sessions := newTestSessionStore(t)
	seen := 0
	for _, r := range buildAPIRoutes(sessions, newWorldImageryHTTPClient()) {
		admin := r.Path == "/api/settings" ||
			strings.HasPrefix(r.Path, "/api/settings/") ||
			strings.HasPrefix(r.Path, "/api/plugins/") ||
			strings.HasPrefix(r.Path, "/api/alarm-transports") ||
			strings.HasPrefix(r.Path, "/api/outside-assistants")
		if !admin {
			continue
		}
		seen++
		if r.Tier != tierAdmin {
			t.Errorf("%s %s is tier %v, want admin (ADR 0040 tier table)", r.Method, r.Path, r.Tier)
		}
	}
	if seen == 0 {
		t.Fatal("no admin-prefixed routes found; the prefix list is stale")
	}
}
