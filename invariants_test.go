package main

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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

// TestCatalogIsFrameworkFree pins the catalog as declarative data (ADR-0003):
// no terminal framework and nothing from this module, so a Spec stays a pure
// value any front end can render and test.
func TestCatalogIsFrameworkFree(t *testing.T) {
	for file, imports := range importsByFile(t, "internal/tui/catalog") {
		for _, imp := range imports {
			if strings.HasPrefix(imp, "github.com/charmbracelet/") || strings.HasPrefix(imp, modulePath+"/") {
				t.Errorf("%s imports %s: the catalog is data; rendering and app state live in internal/tui", file, imp)
			}
		}
	}
}

// TestWorkspaceHasNoTenantAccess pins the workspace file as display defaults
// only (ADR-0005): it cannot reach a tenant, so it cannot carry credentials or
// actions.
func TestWorkspaceHasNoTenantAccess(t *testing.T) {
	for file, imports := range importsByFile(t, "internal/workspace") {
		for _, imp := range imports {
			if imp == "net/http" || strings.HasPrefix(imp, "net/http/") ||
				strings.HasPrefix(imp, "github.com/dynatrace-oss/dtctl") || strings.HasPrefix(imp, modulePath+"/") {
				t.Errorf("%s imports %s: internal/workspace holds display defaults only", file, imp)
			}
		}
	}
}

// agentsMDMaxLines keeps AGENTS.md a routing table, not a manual: every agent
// session pays for each line of it.
const agentsMDMaxLines = 150

var (
	mdLink      = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	codePath    = regexp.MustCompile("`([A-Za-z0-9_./-]+\\.(?:go|md|ya?ml))`")
	codeTest    = regexp.MustCompile("`(Test[A-Za-z0-9_]+)`")
	testFuncDef = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
)

// TestAgentDocsStayAccurate fails when an agent-facing doc names a file or a
// test that no longer exists, so the docs agents start from cannot silently
// drift from the code.
func TestAgentDocsStayAccurate(t *testing.T) {
	docs := []string{"AGENTS.md", "CLAUDE.md", "CONTRIBUTING.md", ".github/pull_request_template.md",
		"docs/README.md", "docs/dev/tech-debt.md"}
	skills, err := filepath.Glob(".agents/skills/*/SKILL.md")
	if err != nil || len(skills) == 0 {
		t.Fatalf("no skills under .agents/skills: %v", err)
	}
	docs = append(docs, skills...)

	baseNames, tests := map[string]bool{}, map[string]bool{}
	err = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".deps" || d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		baseNames[d.Name()] = true
		if strings.HasSuffix(path, "_test.go") {
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range testFuncDef.FindAllStringSubmatch(string(src), -1) {
				tests[m[1]] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	for _, doc := range docs {
		raw, err := os.ReadFile(doc)
		if err != nil {
			t.Errorf("%s: %v", doc, err)
			continue
		}
		text := string(raw)
		if doc == "AGENTS.md" {
			if n := strings.Count(text, "\n"); n > agentsMDMaxLines {
				t.Errorf("AGENTS.md has %d lines (max %d): move detail into a routed doc or skill", n, agentsMDMaxLines)
			}
		}
		for _, m := range mdLink.FindAllStringSubmatch(text, -1) {
			target := strings.SplitN(m[1], "#", 2)[0]
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if !exists(filepath.Join(filepath.Dir(doc), target)) {
				t.Errorf("%s links to %s, which does not exist", doc, m[1])
			}
		}
		for _, m := range codePath.FindAllStringSubmatch(text, -1) {
			p := m[1]
			if !strings.Contains(p, "/") && !strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, ".md") {
				continue // a bare config name such as .dtui.yaml is a user file, not a repo path
			}
			if exists(p) || exists(filepath.Join(filepath.Dir(doc), p)) || (!strings.Contains(p, "/") && baseNames[p]) {
				continue
			}
			t.Errorf("%s names %s, which does not exist", doc, p)
		}
		for _, m := range codeTest.FindAllStringSubmatch(text, -1) {
			if !tests[m[1]] {
				t.Errorf("%s names %s, which is not a test in this repo", doc, m[1])
			}
		}
	}
}
