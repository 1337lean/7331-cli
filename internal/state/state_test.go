package state

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
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
		value string
		token string
	}{
		{id, ""},
		{"https://i.7331.cloud/" + id + ".png", ""},
		{"https://7331.cloud/f/" + id, ""},
		{"https://7331.cloud/d/" + id + "#token=secret", "secret"},
	}
	for _, test := range cases {
		reference, err := ParseReference(test.value)
		if err != nil {
			t.Fatalf("%s: %v", test.value, err)
		}
		if reference.PublicID != id || reference.Token != test.token {
			t.Fatalf("%s: got %#v", test.value, reference)
		}
	}
}
