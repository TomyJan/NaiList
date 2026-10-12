package main

import "runtime/debug"

// Set by scripts/build.mjs (or Docker build arguments) at compilation.
var buildVersion string

func applicationVersion() string {
	if buildVersion != "" {
		return buildVersion
	}
	// Plain go build still identifies its compiled revision without consulting Git at startup.
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && len(setting.Value) >= 7 {
				return Version + "-" + setting.Value[:7]
			}
		}
	}
	return Version + "-dev"
}
