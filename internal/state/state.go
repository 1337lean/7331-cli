package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var publicIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{20,}$`)

type Record struct {
	Version       int    `json:"version"`
	PublicID      string `json:"public_id"`
	DeletionToken string `json:"deletion_token"`
	URL           string `json:"url"`
	DetailsURL    string `json:"details_url"`
	DeletionURL   string `json:"deletion_url"`
	Filename      string `json:"filename"`
	CreatedAt     string `json:"created_at"`
	ExpiresAt     string `json:"expires_at"`
}

type Store struct {
	root string
}

func New(root string) (*Store, error) {
	if root == "" {
		var err error
		root, err = defaultRoot()
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil && runtime.GOOS != "windows" {
		return nil, fmt.Errorf("secure state directory: %w", err)
	}
	return &Store{root: root}, nil
}

func defaultRoot() (string, error) {
	var base string
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "Library", "Application Support")
	case "windows":
		base = os.Getenv("LocalAppData")
		if base == "" {
			var err error
			base, err = os.UserConfigDir()
			if err != nil {
				return "", err
			}
		}
	default:
		base = os.Getenv("XDG_STATE_HOME")
		if base == "" {
			var err error
			base, err = os.UserConfigDir()
			if err != nil {
				return "", err
			}
		}
	}
	return filepath.Join(base, "7331", "uploads"), nil
}

func (s *Store) path(publicID string) (string, error) {
	if !publicIDPattern.MatchString(publicID) {
		return "", errors.New("invalid public ID")
	}
	return filepath.Join(s.root, publicID+".json"), nil
}

func (s *Store) Save(record Record) error {
	target, err := s.path(record.PublicID)
	if err != nil {
		return err
	}
	record.Version = 1
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(s.root, ".upload-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary state file: %w", err)
	}
	name := temporary.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(name)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		_ = temporary.Close()
		return fmt.Errorf("secure temporary state file: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write state file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync state file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close state file: %w", err)
	}
	if err := os.Rename(name, target); err != nil {
		return fmt.Errorf("commit state file: %w", err)
	}
	if err := os.Chmod(target, 0o600); err != nil && runtime.GOOS != "windows" {
		return fmt.Errorf("secure state file: %w", err)
	}
	cleanup = false
	return nil
}

func (s *Store) Load(publicID string) (Record, error) {
	target, err := s.path(publicID)
	if err != nil {
		return Record{}, err
	}
	data, err := os.ReadFile(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Record{}, errors.New("no locally saved deletion credential for this public ID")
		}
		return Record{}, err
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil || record.Version != 1 || record.PublicID != publicID || record.DeletionToken == "" {
		return Record{}, errors.New("saved deletion credential is invalid")
	}
	return record, nil
}

func (s *Store) Remove(publicID string) error {
	target, err := s.path(publicID)
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

type Reference struct {
	PublicID string
	Token    string
}

func ParseReference(value string) (Reference, error) {
	if publicIDPattern.MatchString(value) {
		return Reference{PublicID: value}, nil
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return Reference{}, errors.New("expected a public ID or supported 7331 URL")
	}
	segments := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(segments) == 0 {
		return Reference{}, errors.New("URL does not contain a public ID")
	}
	last, err := url.PathUnescape(segments[len(segments)-1])
	if err != nil {
		return Reference{}, errors.New("URL contains an invalid public ID")
	}
	if dot := strings.LastIndexByte(last, '.'); dot > 0 {
		last = last[:dot]
	}
	if !publicIDPattern.MatchString(last) {
		return Reference{}, errors.New("URL does not contain a valid public ID")
	}
	token := parsed.Fragment
	if values, err := url.ParseQuery(parsed.Fragment); err == nil {
		token = values.Get("token")
	}
	return Reference{PublicID: last, Token: token}, nil
}

func DeletionToken(deletionURL string) (string, error) {
	reference, err := ParseReference(deletionURL)
	if err != nil || reference.Token == "" {
		return "", errors.New("upload response did not include a valid deletion URL")
	}
	return reference.Token, nil
}
