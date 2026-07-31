//go:build darwin || linux

package terminal

import (
	"os"
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

// Darwin requires a pseudoterminal to be granted and unlocked before either
// endpoint answers terminal-attribute requests. Linux needs none of this.
const (
	tiocPtyGrant = 0x20007454
	tiocPtyUnlk  = 0x20007452
	tiocPtyGname = 0x40807453
)

// TestPseudoterminalIsDetected guards the direction the negative tests cannot:
// an ioctl that always failed would report every stream as noninteractive, and
// the drag-and-drop prompt would silently never appear. Every step that can
// fail for environmental reasons skips instead.
func TestPseudoterminalIsDetected(t *testing.T) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudoterminal available: %v", err)
	}
	defer master.Close()

	if runtime.GOOS != "darwin" {
		if !IsTerminal(master) {
			t.Fatal("a pseudoterminal was not detected as a terminal")
		}
		return
	}

	for _, request := range []uintptr{tiocPtyGrant, tiocPtyUnlk} {
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), request, 0); errno != 0 {
			t.Skipf("ioctl %#x: %v", request, errno)
		}
	}
	var name [128]byte
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), tiocPtyGname, uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		t.Skipf("pseudoterminal name: %v", errno)
	}
	end := 0
	for end < len(name) && name[end] != 0 {
		end++
	}
	slave, err := os.OpenFile(string(name[:end]), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("open %s: %v", name[:end], err)
	}
	defer slave.Close()

	if !IsTerminal(slave) {
		t.Fatalf("pseudoterminal %s was not detected as a terminal", name[:end])
	}
}
