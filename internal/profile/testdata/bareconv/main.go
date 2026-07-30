package main

import "github.com/tuntran/agentcodeswitch/internal/profile"

func main() {
	// Must not compile.
	_ = profile.ConfigDirLiteral("/Users/x/.acs/profiles/per")
}
