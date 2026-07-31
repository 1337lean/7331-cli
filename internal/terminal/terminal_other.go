//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package terminal

import "os"

// IsTerminal falls back to the character-device test on platforms without a
// terminal-attribute ioctl. No release target uses this build.
func IsTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
