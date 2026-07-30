package main

import "github.com/tuntran/agentcodeswitch/internal/profile"

func main() {
	// Must not compile: the field is unexported.
	_ = profile.ConfigDirLiteral{s: "/Users/x/.acs/profiles/per"}
}
