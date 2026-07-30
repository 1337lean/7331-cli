package main

import (
	"os"
	"runtime/debug"

	"github.com/1337lean/7331-cli/internal/command"
)

var (
	version   = "dev"
	commit    = ""
	buildDate = ""
)

func main() {
	resolvedVersion, resolvedCommit, resolvedDate := build()
	os.Exit(command.App{
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		StdinTTY:  isTerminal(os.Stdin),
		StdoutTTY: isTerminal(os.Stdout),
		Version:   resolvedVersion,
		Commit:    resolvedCommit,
		BuildDate: resolvedDate,
	}.Run(os.Args[1:]))
}

// build fills in identifying information for binaries produced without release
// ldflags, such as those installed by `go install`. Release values always win.
func build() (string, string, string) {
	if version != "dev" {
		return version, commit, buildDate
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version, commit, buildDate
	}
	resolved, resolvedCommit, resolvedDate := version, commit, buildDate
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		resolved = info.Main.Version
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if resolvedCommit == "" {
				resolvedCommit = setting.Value
			}
		case "vcs.time":
			if resolvedDate == "" {
				resolvedDate = setting.Value
			}
		}
	}
	return resolved, resolvedCommit, resolvedDate
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
