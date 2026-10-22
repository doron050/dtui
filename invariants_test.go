package main

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/dynatrace-oss/dtui"

// allowedTUIDtctlImports are the only dtctl packages internal/tui may use:
// the DQL executor and the renderers. Anything else (resource handlers,
// clients) would be a path to the tenant that bypasses the sources.go seam
// and, with it, the read-only guarantee (ADR-0011).
var allowedTUIDtctlImports = map[string]bool{
	"github.com/dynatrace-oss/dtctl/pkg/exec":   true,
	"github.com/dynatrace-oss/dtctl/pkg/output": true,
}

// importsByFile parses the imports of every non-test Go file under root,
// regardless of build tags (the wasm entry points must be covered too).
func importsByFile(t *testing.T, root string) map[string][]string {
	t.Helper()
	result := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".deps" || d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			result[path] = append(result[path], strings.Trim(imp.Path.Value, `"`))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// TestTUIHasNoDirectTenantAccess pins "no HTTP inside internal/tui" and the
// read-only import graph: every API call goes through an injected Source.
func TestTUIHasNoDirectTenantAccess(t *testing.T) {
	for file, imports := range importsByFile(t, "internal/tui") {
		for _, imp := range imports {
			if imp == "net/http" || strings.HasPrefix(imp, "net/http/") {
				t.Errorf("%s imports %s: internal/tui must not do HTTP; add a Source in sources.go", file, imp)
			}
			if strings.HasPrefix(imp, "github.com/dynatrace-oss/dtctl") && !allowedTUIDtctlImports[imp] {
				t.Errorf("%s imports %s: only pkg/exec and pkg/output are allowed in internal/tui", file, imp)
			}
		}
	}
}

// TestOnlyEntryPointsImportTUI pins the layering rule: the TUI shell is
// imported by the main package and the browser entry point (cmd/web) only.
func TestOnlyEntryPointsImportTUI(t *testing.T) {
	tuiPath := modulePath + "/internal/tui"
	for file, imports := range importsByFile(t, ".") {
		dir := filepath.ToSlash(filepath.Dir(file))
		if dir == "." || dir == "cmd/web" || strings.HasPrefix(dir, "internal/tui") {
			continue
		}
		for _, imp := range imports {
			if imp == tuiPath {
				t.Errorf("%s imports internal/tui: only the main package and cmd/web may", file)
			}
		}
	}
}
