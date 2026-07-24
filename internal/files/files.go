package files

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode"
)

const MaxBytes int64 = 25 * 1024 * 1024

type File struct {
	Path     string
	Filename string
	MIMEType string
	Size     int64
}

func Validate(paths []string) ([]File, error) {
	if len(paths) < 1 || len(paths) > 5 {
		return nil, fmt.Errorf("upload requires between one and five files")
	}
	validated := make([]File, 0, len(paths))
	for _, path := range paths {
		file, err := validate(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		validated = append(validated, file)
	}
	return validated, nil
}

func validate(path string) (File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return File{}, err
	}
	if !info.Mode().IsRegular() {
		return File{}, errors.New("not a regular file")
	}
	if info.Size() == 0 {
		return File{}, errors.New("file is empty")
	}
	if info.Size() > MaxBytes {
		return File{}, fmt.Errorf("file exceeds 25 MiB")
	}
	for _, character := range filepath.Base(path) {
		if unicode.IsControl(character) {
			return File{}, errors.New("filename contains a control character")
		}
	}
	handle, err := os.Open(path)
	if err != nil {
		return File{}, err
	}
	defer handle.Close()
	var header [12]byte
	count, err := io.ReadFull(handle, header[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return File{}, err
	}
	mime, ok := detectMIME(header[:count])
	if !ok {
		return File{}, errors.New("unsupported image format")
	}
	return File{
		Path:     path,
		Filename: filepath.Base(path),
		MIMEType: mime,
		Size:     info.Size(),
	}, nil
}

func detectMIME(header []byte) (string, bool) {
	switch {
	case len(header) >= 3 && bytes.Equal(header[:3], []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg", true
	case len(header) >= 8 && bytes.Equal(header[:8], []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}):
		return "image/png", true
	case len(header) >= 6 && (bytes.Equal(header[:6], []byte("GIF87a")) || bytes.Equal(header[:6], []byte("GIF89a"))):
		return "image/gif", true
	case len(header) >= 12 && bytes.Equal(header[:4], []byte("RIFF")) && bytes.Equal(header[8:12], []byte("WEBP")):
		return "image/webp", true
	default:
		return "", false
	}
}
