package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// cmdUI launches the desktop app.
func cmdUI(args []string) error {
	if len(args) > 0 {
		return errors.New("usage: acs ui")
	}
	bundle, err := findAppBundle()
	if err != nil {
		return err
	}
	// `open` hands off to Launch Services and returns immediately, which is what
	// keeps the terminal usable after starting the app.
	out, err := exec.Command("open", bundle).CombinedOutput()
	if err != nil {
		return fmt.Errorf("open %s: %w: %s", bundle, err, out)
	}
	fmt.Println("launched " + bundle)
	return nil
}

// findAppBundle locates acs.app.
//
// The build output is checked last so an installed copy wins over a stale one left
// in build/bin from an old `wails build`.
func findAppBundle() (string, error) {
	var candidates []string
	if fromEnv := os.Getenv("ACS_APP"); fromEnv != "" {
		candidates = append(candidates, fromEnv)
	}
	candidates = append(candidates,
		"/Applications/acs.app",
		filepath.Join(os.Getenv("HOME"), "Applications", "acs.app"))
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "acs.app"),
			filepath.Join(dir, "..", "acs.app"))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "build", "bin", "acs.app"))
	}

	for _, candidate := range candidates {
		resolved, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(resolved); err == nil && info.IsDir() {
			return resolved, nil
		}
	}
	return "", errors.New(
		"acs.app not found. Build it with `wails build` (the bundle lands in build/bin/), " +
			"move it to /Applications, or set ACS_APP to its path")
}
