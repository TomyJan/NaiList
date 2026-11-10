package main

import "runtime/debug"

// Set by scripts/build.mjs (or Docker build arguments) at compilation.
var buildVersion string
var buildCommit string

func applicationCommit() string {
	if buildCommit != "" {
		return buildCommit
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return setting.Value
			}
		}
	}
	return ""
}

func applicationVersion() string {
	if buildVersion != "" {
		return buildVersion
	}
	// Plain go build still identifies its compiled revision without consulting Git at startup.
	if commit := applicationCommit(); len(commit) >= 7 {
		return Version + "-" + commit[:7]
	}
	return Version + "-dev"
}
