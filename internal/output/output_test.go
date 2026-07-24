package output

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/1337lean/7331-cli/internal/api"
)

func TestUploadInteractiveGolden(t *testing.T) {
	t.Parallel()
	upload := api.Upload{
		Source:      "image.png",
		PublicID:    "public_identifier_0001",
		URL:         "https://i.7331.cloud/public.png",
		DetailsURL:  "https://7331.cloud/f/public",
		DeletionURL: "https://7331.cloud/d/public_identifier_0001#token=secret",
		ExpiresAt:   "2026-07-25T18:00:00Z",
	}
	for _, test := range []struct {
		name       string
		showDelete bool
		golden     string
	}{
		{"command", false, "upload_interactive.golden"},
		{"deletion URL", true, "upload_delete_url.golden"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var actual bytes.Buffer
			UploadInteractive(&actual, upload, test.showDelete)
			expected, err := os.ReadFile(filepath.Join("testdata", test.golden))
			if err != nil {
				t.Fatal(err)
			}
			if actual.String() != string(expected) {
				t.Fatalf("output mismatch\n--- got ---\n%s--- want ---\n%s", actual.String(), expected)
			}
		})
	}
}
