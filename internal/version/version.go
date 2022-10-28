package version

import (
	"runtime"
	"runtime/debug"
)

var (
	// Name is the name of the binary.
	Name = "gcr-cleaner"

	// Version is the main package version.
	Version = "source"

	// Commit is the git sha.
	Commit = "HEAD"

	// OSArch is the operating system and architecture combination.
	OSArch = runtime.GOOS + "/" + runtime.GOARCH

	// HumanVersion is the compiled version.
	HumanVersion = func() string {
		version := Version
		if version == "" {
			version = "source"
		}

		commit := Commit
		if commit == "" {
			if info, ok := debug.ReadBuildInfo(); ok {
				for _, setting := range info.Settings {
					if setting.Key == "vcs.revision" {
						return setting.Value
					}
				}
			}
		}
		if commit == "" {
			commit = "unknown"
		}

		return Name + " " + version + " (" + commit + ", " + OSArch + ")"
	}()
)
