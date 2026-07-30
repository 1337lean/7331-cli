package state

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestStoreSavesAtomicOwnerOnlyRecordsConcurrently(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "uploads")
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for index := 0; index < 20; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			id := fmt.Sprintf("public_identifier_%04d", index)
			if err := store.Save(Record{
				PublicID:      id,
				DeletionToken: fmt.Sprintf("secret-%d", index),
			}); err != nil {
				t.Errorf("save %d: %v", index, err)
			}
		}(index)
	}
	wait.Wait()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 20 {
		t.Fatalf("got %d records, want 20", len(entries))
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			t.Fatalf("temporary file remained: %s", entry.Name())
		}
		if runtime.GOOS != "windows" {
			info, err := entry.Info()
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("%s mode = %o, want 600", entry.Name(), info.Mode().Perm())
			}
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(root)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("directory mode = %o, want 700", info.Mode().Perm())
		}
	}
	record, err := store.Load("public_identifier_0007")
	if err != nil {
		t.Fatal(err)
	}
	if record.DeletionToken != "secret-7" {
		t.Fatalf("unexpected token %q", record.DeletionToken)
	}
}

func TestParseReference(t *testing.T) {
	t.Parallel()
	id := "abcdefghijklmnopqrst"
	cases := []struct {
		value  string
		token  string
		origin string
	}{
		{id, "", ""},
		{"https://i.7331.cloud/" + id + ".png", "", "https://i.7331.cloud"},
		{"https://7331.cloud/f/" + id, "", "https://7331.cloud"},
		{"https://7331.cloud/d/" + id + "#token=secret", "secret", "https://7331.cloud"},
		{"http://127.0.0.1:3000/d/" + id + "#token=secret", "secret", "http://127.0.0.1:3000"},
	}
	for _, test := range cases {
		reference, err := ParseReference(test.value)
		if err != nil {
			t.Fatalf("%s: %v", test.value, err)
		}
		if reference.PublicID != id || reference.Token != test.token || reference.Origin() != test.origin {
			t.Fatalf("%s: got %#v origin %q", test.value, reference, reference.Origin())
		}
	}
}

func TestListSortsNewestFirstAndSkipsForeignFiles(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "uploads")
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	for index, created := range []string{"2026-07-01T00:00:00Z", "2026-07-03T00:00:00Z", "2026-07-02T00:00:00Z"} {
		if err := store.Save(Record{
			PublicID:      fmt.Sprintf("public_identifier_%04d", index),
			DeletionToken: "secret",
			CreatedAt:     created,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	records, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(records))
	for _, record := range records {
		got = append(got, record.CreatedAt)
	}
	want := "[2026-07-03T00:00:00Z 2026-07-02T00:00:00Z 2026-07-01T00:00:00Z]"
	if fmt.Sprint(got) != want {
		t.Fatalf("order = %v, want %s", got, want)
	}
}

func TestListOnMissingDirectoryIsEmpty(t *testing.T) {
	t.Parallel()
	store := &Store{root: filepath.Join(t.TempDir(), "never-created")}
	records, err := store.List()
	if err != nil || len(records) != 0 {
		t.Fatalf("records = %v, err = %v", records, err)
	}
}

func TestPruneRemovesExpiredAndKeepsUnparsableExpiry(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "uploads")
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	records := map[string]string{
		"public_identifier_0001": now.Add(-time.Hour).Format(time.RFC3339),
		"public_identifier_0002": now.Add(time.Hour).Format(time.RFC3339),
		"public_identifier_0003": "",
		"public_identifier_0004": "not a timestamp",
	}
	for id, expires := range records {
		if err := store.Save(Record{PublicID: id, DeletionToken: "secret", ExpiresAt: expires}); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := store.Prune(now)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := store.Load("public_identifier_0001"); err == nil {
		t.Fatal("expired record survived")
	}
	for _, id := range []string{"public_identifier_0002", "public_identifier_0003", "public_identifier_0004"} {
		if _, err := store.Load(id); err != nil {
			t.Fatalf("%s was pruned: %v", id, err)
		}
	}
}
