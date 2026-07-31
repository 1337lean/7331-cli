package terminal

import (
	"os"
	"testing"
)

// A character device that is not a terminal is the case that matters: the
// previous character-device heuristic turned `7331 upload </dev/null` into an
// interactive prompt.
func TestCharacterDevicesAreNotTerminals(t *testing.T) {
	for _, name := range []string{os.DevNull, "/dev/zero"} {
		file, err := os.Open(name)
		if err != nil {
			continue
		}
		if IsTerminal(file) {
			t.Errorf("%s reported as a terminal", name)
		}
		_ = file.Close()
	}
}

func TestPipesAndFilesAreNotTerminals(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if IsTerminal(reader) || IsTerminal(writer) {
		t.Error("pipe reported as a terminal")
	}

	regular, err := os.CreateTemp(t.TempDir(), "regular")
	if err != nil {
		t.Fatal(err)
	}
	defer regular.Close()
	if IsTerminal(regular) {
		t.Error("regular file reported as a terminal")
	}

	if IsTerminal(nil) {
		t.Error("nil file reported as a terminal")
	}
}
