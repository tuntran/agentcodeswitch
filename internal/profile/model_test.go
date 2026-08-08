package profile

import (
	"errors"
	"strings"
	"testing"

	"github.com/tuntran/agentcodeswitch/internal/config"
)

func TestValidateModel(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Empty is the "no default" setting, not a missing value.
		{"empty clears the default", "", false},
		{"plain id", "claude-opus-5", false},
		{"dated id", "claude-haiku-4-5-20251001", false},
		// Bracketed and slashed forms are accepted because acs has no list of
		// models to check against, and a whitelist would reject every model
		// released after this binary was built.
		{"context variant", "claude-opus-5[1m]", false},
		{"vendor-prefixed id", "us.anthropic.claude-opus-4-5-v1:0", false},
		{"unreleased id acs has never heard of", "claude-something-9", false},

		{"pasted assignment", "ANTHROPIC_MODEL=claude-opus-5", true},
		{"embedded space", "claude opus 5", true},
		{"newline", "claude-opus-5\n", true},
		{"tab", "claude\t5", true},
		{"control character", "claude-opus-5\x00", true},
		{"too long", strings.Repeat("a", maxModelLen+1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateModel(tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateModel(%q) = nil, want an error", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateModel(%q) = %v, want nil", tt.input, err)
			}
		})
	}
}

func TestModelID(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		context1M bool
		want      string
	}{
		{"suffix appended by default", "claude-opus-4-8", true, "claude-opus-4-8[1m]"},
		// Turning it off has to give the plain name back. Anything else leaves a
		// mangled id behind and the option is not reversible.
		{"off gives the plain name back", "claude-opus-4-8", false, "claude-opus-4-8"},
		// There is nothing to suffix, and "[1m]" alone is not a model.
		{"no model, option on", "", true, ""},
		{"no model, option off", "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Profile{Model: tt.model, Context1M: tt.context1M}
			if got := p.ModelID(); got != tt.want {
				t.Errorf("ModelID() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Extended context is on by default, and the default has to survive a config file
// written before the option existed -- hence the inverted field on disk.
func TestContext1MDefaultsOnForAnEntryWithoutTheField(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())
	p, err := Create("per", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !p.Context1M {
		t.Error("Create reported extended context off; the default is on")
	}
	got, err := Get("per")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Context1M {
		t.Error("a profile written without the field reads back with extended context off")
	}
}

func TestSetContext1M(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())
	if _, err := Create("per", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := SetModel("per", "claude-opus-4-8"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}

	if err := SetContext1M("per", false); err != nil {
		t.Fatalf("SetContext1M: %v", err)
	}
	p, err := Get("per")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Context1M {
		t.Error("Context1M is still on after turning it off")
	}
	// The model name is the toggle's business only through ModelID.
	if p.Model != "claude-opus-4-8" {
		t.Errorf("Model = %q, want the name unchanged", p.Model)
	}
	if p.ModelID() != "claude-opus-4-8" {
		t.Errorf("ModelID() = %q, want no suffix", p.ModelID())
	}

	if err := SetContext1M("per", true); err != nil {
		t.Fatalf("SetContext1M back on: %v", err)
	}
	p, _ = Get("per")
	if p.ModelID() != "claude-opus-4-8[1m]" {
		t.Errorf("ModelID() = %q, want the suffix back", p.ModelID())
	}
}

// Typing the suffixed form is what the Claude Code docs show, so it has to mean
// "this model, extended context" -- not a name that gets suffixed again.
func TestSetModelSplitsTheContextSuffix(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())
	if _, err := Create("per", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := SetContext1M("per", false); err != nil {
		t.Fatalf("SetContext1M: %v", err)
	}

	if err := SetModel("per", "claude-opus-4-8[1m]"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	p, err := Get("per")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Model != "claude-opus-4-8" {
		t.Errorf("Model = %q, want the suffix stripped from the stored name", p.Model)
	}
	if !p.Context1M {
		t.Error("a pasted [1m] did not turn the option on")
	}
	if p.ModelID() != "claude-opus-4-8[1m]" {
		t.Errorf("ModelID() = %q, want exactly one suffix", p.ModelID())
	}
}

// A doubled suffix has to collapse. Cutting one leaves "[1m]" inside the stored
// name, and ModelID then emits "...[1m][1m]" -- an id claude rejects, while the UI
// redraws as "...[1m]" and looks correct.
func TestSetModelCollapsesRepeatedContextSuffix(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())
	if _, err := Create("per", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := SetModel("per", "claude-opus-4-8[1m][1m]"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	p, err := Get("per")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Model != "claude-opus-4-8" {
		t.Errorf("Model = %q, want the name with no suffix left in it", p.Model)
	}
	if p.ModelID() != "claude-opus-4-8[1m]" {
		t.Errorf("ModelID() = %q, want exactly one suffix", p.ModelID())
	}
}

// config.json is a file people edit. A suffix that never went through SetModel
// still has to come out of resolve stripped, or ModelID suffixes it again.
func TestGetSplitsASuffixWrittenByHand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ACS_HOME", home)
	if _, err := Create("per", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	err := config.Update(func(f *config.File) error {
		e := f.Profiles["per"]
		e.Model = "claude-opus-4-8[1m][1m]"
		e.NoContext1M = true
		f.Profiles["per"] = e
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	p, err := Get("per")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Model != "claude-opus-4-8" {
		t.Errorf("Model = %q, want the suffix stripped on read", p.Model)
	}
	// The suffix in the file is the more explicit statement of the two, so it
	// turns the option on the same way SetModel does.
	if !p.Context1M {
		t.Error("a suffix in the file did not turn the option on")
	}
	if p.ModelID() != "claude-opus-4-8[1m]" {
		t.Errorf("ModelID() = %q, want exactly one suffix", p.ModelID())
	}
}

// Renaming the model is the common path from the UI while the option is off, and
// it must not quietly turn extended context back on.
func TestSetModelLeavesTheOptionAloneWithoutASuffix(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())
	if _, err := Create("per", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := SetContext1M("per", false); err != nil {
		t.Fatalf("SetContext1M: %v", err)
	}
	if err := SetModel("per", "claude-haiku-4-5-20251001"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	p, err := Get("per")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Context1M {
		t.Error("renaming the model turned extended context back on")
	}
	if p.ModelID() != "claude-haiku-4-5-20251001" {
		t.Errorf("ModelID() = %q, want no suffix", p.ModelID())
	}
}

func TestSetModelRoundTrip(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())
	if _, err := Create("per", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Surrounding whitespace comes from pasting, not from intent, so it is
	// trimmed rather than rejected.
	if err := SetModel("per", "  claude-opus-5  "); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	p, err := Get("per")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Model != "claude-opus-5" {
		t.Errorf("Model = %q, want %q", p.Model, "claude-opus-5")
	}

	// Clearing has to be reachable: without it a profile pinned to a retired
	// model could never be handed back to Claude Code's own default.
	if err := SetModel("per", ""); err != nil {
		t.Fatalf("SetModel clear: %v", err)
	}
	p, err = Get("per")
	if err != nil {
		t.Fatalf("Get after clear: %v", err)
	}
	if p.Model != "" {
		t.Errorf("Model = %q after clearing, want empty", p.Model)
	}
}

func TestSetModelRejectsUnknownProfile(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())
	err := SetModel("nope", "claude-opus-5")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("SetModel on a missing profile = %v, want ErrNotFound", err)
	}
}

// A rejected model must not reach disk: a half-validated write would leave the
// registry holding a value that Environ then exports verbatim.
func TestSetModelDoesNotStoreInvalid(t *testing.T) {
	t.Setenv("ACS_HOME", t.TempDir())
	if _, err := Create("per", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := SetModel("per", "claude opus"); err == nil {
		t.Fatal("SetModel accepted a model id with a space")
	}
	p, err := Get("per")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Model != "" {
		t.Errorf("Model = %q, want empty after a rejected save", p.Model)
	}
}
