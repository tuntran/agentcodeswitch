package profile_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestConfigDirLiteralCannotBeForged is the test that keeps LBD #1 honest.
//
// The protection is that ConfigDirLiteral is a struct with an unexported field.
// A `type ConfigDirLiteral string` would look equally safe and stop nothing:
// profile.ConfigDirLiteral(rawString) compiles from any package, and that bare
// conversion is exactly what someone reaches for to quiet the compiler on a
// deadline.
//
// The two fixtures live under testdata/ so they are not part of any build. This
// test compiles them on purpose and asserts the compiler refuses.
func TestConfigDirLiteralCannotBeForged(t *testing.T) {
	tests := []struct {
		pkg      string
		wantText string
	}{
		{"./testdata/bareconv", "cannot convert"},
		{"./testdata/barestruct", "unexported field"},
	}
	for _, tt := range tests {
		t.Run(tt.pkg, func(t *testing.T) {
			// #nosec G204 -- fixed literals from the table above.
			out, err := exec.Command("go", "build", "-o", "/dev/null", tt.pkg).CombinedOutput()
			if err == nil {
				t.Fatalf("%s compiled; a ConfigDirLiteral can be forged from a bare string", tt.pkg)
			}
			if !strings.Contains(string(out), tt.wantText) {
				t.Errorf("%s failed for the wrong reason:\n%s", tt.pkg, out)
			}
		})
	}
}
