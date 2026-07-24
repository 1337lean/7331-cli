package main

import (
	"os"

	"github.com/1337lean/7331-cli/internal/command"
)

var (
	version   = "dev"
	commit    = ""
	buildDate = ""
)

func main() {
	os.Exit(command.App{
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		StdinTTY:  isTerminal(os.Stdin),
		StdoutTTY: isTerminal(os.Stdout),
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
	}.Run(os.Args[1:]))
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
