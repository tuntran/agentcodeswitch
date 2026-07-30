package main

import (
	"flag"
	"io"
	"slices"
	"testing"
)

// TestParseFlagsAcceptsFlagsAfterPositionals is a regression test for a real bug.
//
// Go's flag package stops parsing at the first non-flag argument, so
// `acs rm per --purge` left --purge sitting in Args() and rm rejected the whole
// invocation. The dangerous version of that mistake would have been a --purge that
// parsed as a positional and got silently ignored.
func TestParseFlagsAcceptsFlagsAfterPositionals(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		wantPurge      bool
		wantLabel      string
		wantPositional []string
	}{
		{
			name:           "flag after positional",
			args:           []string{"per", "--purge"},
			wantPurge:      true,
			wantPositional: []string{"per"},
		},
		{
			name:           "flag before positional still works",
			args:           []string{"--purge", "per"},
			wantPurge:      true,
			wantPositional: []string{"per"},
		},
		{
			name:           "no flags",
			args:           []string{"per"},
			wantPositional: []string{"per"},
		},
		{
			name:           "value flag after positional",
			args:           []string{"per", "--label", "Personal"},
			wantLabel:      "Personal",
			wantPositional: []string{"per"},
		},
		{
			name:           "inline value flag after positional",
			args:           []string{"per", "--label=Personal"},
			wantLabel:      "Personal",
			wantPositional: []string{"per"},
		},
		{
			name:           "mixed order",
			args:           []string{"--label", "Work", "per", "--purge"},
			wantPurge:      true,
			wantLabel:      "Work",
			wantPositional: []string{"per"},
		},
		{
			name:           "single dash flag form",
			args:           []string{"per", "-purge"},
			wantPurge:      true,
			wantPositional: []string{"per"},
		},
		{
			// A value that looks like a positional must not be stolen from its flag.
			name:           "flag value is not treated as positional",
			args:           []string{"--label", "per", "com"},
			wantLabel:      "per",
			wantPositional: []string{"com"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			purge := fs.Bool("purge", false, "")
			label := fs.String("label", "", "")

			if err := parseFlags(fs, tt.args); err != nil {
				t.Fatalf("parseFlags(%q): %v", tt.args, err)
			}
			if *purge != tt.wantPurge {
				t.Errorf("purge = %v, want %v", *purge, tt.wantPurge)
			}
			if *label != tt.wantLabel {
				t.Errorf("label = %q, want %q", *label, tt.wantLabel)
			}
			if !slices.Equal(fs.Args(), tt.wantPositional) {
				t.Errorf("positional = %q, want %q", fs.Args(), tt.wantPositional)
			}
		})
	}
}

// TestParseFlagsStopsAtTerminator keeps `--` meaning "the rest is not mine".
func TestParseFlagsStopsAtTerminator(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	purge := fs.Bool("purge", false, "")

	if err := parseFlags(fs, []string{"per", "--", "--purge"}); err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if *purge {
		t.Error("purge was set from an argument after --")
	}
	if want := []string{"per", "--purge"}; !slices.Equal(fs.Args(), want) {
		t.Errorf("positional = %q, want %q", fs.Args(), want)
	}
}

// TestParseFlagsTerminatorFirst: stripping `--` and handing the rest to flag.Parse
// turned the protected arguments straight back into flags.
func TestParseFlagsTerminatorFirst(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	purge := fs.Bool("purge", false, "")

	if err := parseFlags(fs, []string{"--", "--purge"}); err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if *purge {
		t.Error("purge was set from an argument after -- with nothing before it")
	}
	if want := []string{"--purge"}; !slices.Equal(fs.Args(), want) {
		t.Errorf("positional = %q, want %q", fs.Args(), want)
	}
}

func TestParseFlagsReportsUnknownFlag(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Bool("purge", false, "")

	if err := parseFlags(fs, []string{"per", "--nope"}); err == nil {
		t.Error("parseFlags accepted an unknown flag")
	}
}
