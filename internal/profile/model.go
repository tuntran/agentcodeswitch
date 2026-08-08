package profile

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/tuntran/agentcodeswitch/internal/config"
)

// maxModelLen is a sanity bound, not a spec. Real ids are far shorter; this only
// stops a paste accident from becoming a permanent environment variable.
const maxModelLen = 200

// contextSuffix is what Claude Code appends to a model alias or full model name to
// ask for the 1M-token context window, as in `claude-opus-4-8[1m]`.
const contextSuffix = "[1m]"

// ModelID is the string handed to Claude Code as ANTHROPIC_MODEL, or "" when the
// profile names no model.
//
// The suffix is applied here rather than stored, so turning the option off gives
// the plain name back instead of leaving a mangled id behind.
//
// It is appended blindly, without checking whether this particular model has a 1M
// window. acs has no model table and refuses to grow one: a list of which models
// support extended context goes stale exactly like a whitelist of model names
// would, and being wrong here would block a model rather than merely mis-size its
// window. Turning the option off per profile is the answer for a model that has no
// 1M variant.
func (p Profile) ModelID() string {
	if p.Model == "" {
		return ""
	}
	if !p.Context1M {
		return p.Model
	}
	return p.Model + contextSuffix
}

// SplitContextSuffix separates a model id from a trailing "[1m]".
//
// Typing or pasting the suffixed form is the obvious thing to do -- it is what the
// Claude Code docs show -- so it is understood as "this model, extended context"
// rather than stored verbatim and suffixed again into `...[1m][1m]`.
//
// It strips every trailing occurrence, not one. Cutting a single suffix leaves the
// second half of `...[1m][1m]` in the stored name, which ModelID then suffixes
// again into an id claude rejects -- while the UI redraws as `...[1m]` with the box
// ticked and looks correct. Stripping to a fixed point is what makes
// Profile.Model's "never carries the suffix" invariant true for every input.
func SplitContextSuffix(model string) (base string, extended bool) {
	base = model
	for {
		trimmed, found := strings.CutSuffix(base, contextSuffix)
		if !found {
			return base, extended
		}
		base, extended = trimmed, true
	}
}

// ValidateModel checks a model id.
//
// The rules are about what an environment value can hold, NOT about which models
// exist. There is no list to check against, and a whitelist would mean acs blocks
// every model released after the binary was built -- exactly when you most want to
// switch to it. So a new id is typeable on its first day and a wrong one surfaces
// as claude's own error, which names the model and is more use than ours would be.
//
// An empty string is valid and means "no default": see config.Entry.Model.
func ValidateModel(model string) error {
	if model == "" {
		return nil
	}
	if len(model) > maxModelLen {
		return fmt.Errorf("model id is longer than %d characters", maxModelLen)
	}
	for _, r := range model {
		switch {
		case r == '=':
			// Almost always a pasted "ANTHROPIC_MODEL=opus". Saying so beats
			// exporting a variable whose value is another assignment.
			return fmt.Errorf("model id %q contains %q: paste the id alone, without the variable name", model, "=")
		case unicode.IsSpace(r) || !unicode.IsPrint(r):
			return fmt.Errorf("model id %q contains whitespace or a control character", model)
		}
	}
	return nil
}

// SetModel records a profile's default model. An empty model clears it, which
// restores Claude Code's own default rather than pinning some other model.
//
// A "[1m]" suffix in the input turns the extended-context option on and is not
// kept in the name: the option is the one place that decides it.
func SetModel(name, model string) error {
	model, extended := SplitContextSuffix(strings.TrimSpace(model))
	if err := ValidateModel(model); err != nil {
		return err
	}
	return config.Update(func(f *config.File) error {
		e, ok := f.Profiles[name]
		if !ok {
			return fmt.Errorf("%w: %q", ErrNotFound, name)
		}
		e.Model = model
		if extended {
			e.NoContext1M = false
		}
		f.Profiles[name] = e
		return nil
	})
}

// SetContext1M turns the 1M-token context window on or off for a profile.
func SetContext1M(name string, on bool) error {
	return config.Update(func(f *config.File) error {
		e, ok := f.Profiles[name]
		if !ok {
			return fmt.Errorf("%w: %q", ErrNotFound, name)
		}
		e.NoContext1M = !on
		f.Profiles[name] = e
		return nil
	})
}
