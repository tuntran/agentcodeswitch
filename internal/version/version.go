// Package version holds the acs version string.
//
// It lives here because both binaries need it -- the CLI prints it and the desktop
// app puts it in the usage endpoint's User-Agent -- and two `package main`
// declarations cannot share a constant.
package version

// Current is the acs version.
const Current = "0.1.0-dev"
