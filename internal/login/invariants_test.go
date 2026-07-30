package login

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// repoRoot walks up to the directory holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// goSourcePaths lists every non-test Go file in the module.
func goSourcePaths(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "build", "node_modules", "testdata", ".git":
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk sources: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("no Go sources found; the invariant checks below would pass vacuously")
	}
	return out
}

// stringLiterals returns every string constant in a file, keyed by position.
//
// Parsing rather than grepping matters here: the comments in this codebase
// deliberately name the things that must not be used, explaining why. A textual
// search flags the explanation and the offence alike.
func stringLiterals(t *testing.T, path string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	out := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		out[fset.Position(lit.Pos()).String()] = value
		return true
	})
	return out
}

// TestNoConsoleLoginAnywhere: --console authenticates for API billing, so the
// resulting token has no user:profile scope and the usage endpoint answers 200 with
// an empty body. Quota would then read 0% on a healthy account, which is the number
// that makes someone start a big task right before being blocked.
func TestNoConsoleLoginAnywhere(t *testing.T) {
	forbidden := []string{"--console", "setup-token"}
	for _, path := range goSourcePaths(t) {
		for pos, value := range stringLiterals(t, path) {
			for _, needle := range forbidden {
				if strings.Contains(value, needle) {
					t.Errorf("%s passes %q; login must always use --claudeai", pos, needle)
				}
			}
		}
	}
}

// TestNeverWritesCredentials: acs reads and deletes credentials but never creates
// one. Writing the Keychain blob by hand can overwrite a login that works, and
// minting tokens with Claude Code's client ID is what the policy restricts.
func TestNeverWritesCredentials(t *testing.T) {
	forbidden := []string{"add-generic-password", "oauth/authorize", "v1/oauth/token"}
	for _, path := range goSourcePaths(t) {
		for pos, value := range stringLiterals(t, path) {
			for _, needle := range forbidden {
				if strings.Contains(value, needle) {
					t.Errorf("%s contains %q; acs must not create credentials", pos, needle)
				}
			}
		}
	}
}

// TestTokensAreNeverPrinted: an error message or log line can end up in a shell
// history, an issue, or a screenshot.
func TestTokensAreNeverPrinted(t *testing.T) {
	printers := []string{
		"Print", "Printf", "Println", "Fprint", "Fprintf", "Fprintln", "Sprintf", "log.",
	}
	for _, path := range goSourcePaths(t) {
		raw, err := os.ReadFile(path) // #nosec G304 -- walking the module's own sources
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if !strings.Contains(line, "AccessToken") && !strings.Contains(line, "accessToken") {
				continue
			}
			for _, p := range printers {
				if strings.Contains(line, p) {
					t.Errorf("%s:%d prints a token: %s", path, i+1, trimmed)
				}
			}
		}
	}
}
