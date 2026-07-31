package terminal

import (
	"os"
	"syscall"
)

// IsTerminal reports whether the file is an interactive console. A console
// handle is the only handle GetConsoleMode accepts, so NUL, pipes, and files
// are all correctly reported as noninteractive.
func IsTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(file.Fd()), &mode) == nil
}
