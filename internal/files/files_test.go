package files

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateDetectsMagicBytes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		data []byte
		mime string
	}{
		{"jpeg", []byte{0xff, 0xd8, 0xff, 0xe0}, "image/jpeg"},
		{"png", []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, "image/png"},
		{"gif87a", []byte("GIF87a payload"), "image/gif"},
		{"gif89a", []byte("GIF89a payload"), "image/gif"},
		{"webp", []byte("RIFF\x04\x00\x00\x00WEBP"), "image/webp"},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "spoofed.txt")
			if err := os.WriteFile(path, test.data, 0o600); err != nil {
				t.Fatal(err)
			}
			result, err := Validate([]string{path})
			if err != nil {
				t.Fatal(err)
			}
			if result[0].MIMEType != test.mime {
				t.Fatalf("got %q, want %q", result[0].MIMEType, test.mime)
			}
		})
	}
}

func TestValidateRejectsUnsupportedAndWrongCounts(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "fake.png")
	if err := os.WriteFile(path, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate([]string{path}); err == nil {
		t.Fatal("expected unsupported image to fail")
	}
	if _, err := Validate(nil); err == nil {
		t.Fatal("expected zero files to fail")
	}
	if _, err := Validate([]string{"1", "2", "3", "4", "5", "6"}); err == nil {
		t.Fatal("expected six files to fail before stat calls")
	}
}

func TestValidateRejectsOversizedFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "large.png")
	handle, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Truncate(MaxBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate([]string{path}); err == nil {
		t.Fatal("expected oversized file to fail")
	}
}

func TestValidateRejectsSymlinks(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	target := filepath.Join(directory, "target.png")
	link := filepath.Join(directory, "link.png")
	if err := os.WriteFile(target, []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Validate([]string{link}); err == nil {
		t.Fatal("expected symlink to fail regular-file validation")
	}
}

func TestValidatedFileSurvivesPathReplacement(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "image.png")
	original := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	validated, err := Validate([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	defer validated[0].Close()
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	handle, err := validated[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(handle)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("read replacement content %q", got)
	}
}
