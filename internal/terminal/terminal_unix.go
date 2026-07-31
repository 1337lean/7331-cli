//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package terminal

import (
	"os"
	"syscall"
	"unsafe"
)

// IsTerminal reports whether the file is an interactive terminal.
//
// The check is a terminal-attribute ioctl rather than a character-device test.
// /dev/null and /dev/zero are character devices, so the cheaper test reports
// `7331 upload </dev/null` as interactive and answers a scripted invocation
// with a drag-and-drop prompt instead of the documented usage error.
func IsTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	var attributes syscall.Termios
	_, _, errno := syscall.Syscall6(
		syscall.SYS_IOCTL,
		file.Fd(),
		uintptr(ioctlReadTermios),
		uintptr(unsafe.Pointer(&attributes)),
		0, 0, 0,
	)
	return errno == 0
}
