package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1337lean/7331-cli/internal/api"
	"github.com/1337lean/7331-cli/internal/files"
	"github.com/1337lean/7331-cli/internal/output"
	"github.com/1337lean/7331-cli/internal/state"
)

func testApp(server *httptest.Server, stateRoot string, stdoutTTY, stdinTTY bool) (*App, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	return &App{
		Stdin:      strings.NewReader("yes\n"),
		Stdout:     &stdout,
		Stderr:     &stderr,
		StdinTTY:   stdinTTY,
		StdoutTTY:  stdoutTTY,
		HTTPClient: server.Client(),
		StateRoot:  stateRoot,
		Version:    "test",
	}, &stdout, &stderr
}

func pngFile(t *testing.T, directory, name string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func uploadServer(t *testing.T, failTicket int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var uploads atomic.Int32
	var tickets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/cli/upload-tickets":
			current := int(tickets.Add(1))
			if current == failTicket {
				writer.Header().Set("Content-Type", "application/problem+json")
				writer.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(writer, `{"status":503,"detail":"tickets paused","request_id":"req-fail"}`)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(writer, `{"data":{"ticket":"ticket-%d","expires_in":300,"max_bytes":8}}`, current)
		case "/api/v1/images":
			current := uploads.Add(1)
			part, err := request.MultipartReader()
			if err != nil {
				t.Errorf("multipart reader: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			file, err := part.NextPart()
			if err != nil {
				t.Errorf("multipart part: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			if file.Header.Get("Content-Type") != "image/png" {
				t.Errorf("content type = %q", file.Header.Get("Content-Type"))
			}
			_, _ = io.Copy(io.Discard, file)
			id := fmt.Sprintf("public_identifier_%04d", current)
			writer.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(writer, `{"data":{"public_id":%q,"url":%q,"details_url":%q,"thumbnail_url":%q,"deletion_url":%q,"mime_type":"image/png","size_bytes":8,"width":1,"height":1,"animated":false,"created_at":"2026-07-24T00:00:00Z","expires_at":"2026-07-25T00:00:00Z"}}`,
				id, "https://i.test/"+id+".png", "https://test/f/"+id, "https://i.test/t/"+id+".webp", "https://test/d/"+id+"#token=secret-"+id)
		default:
			t.Errorf("unexpected path %s", request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	return server, &uploads
}

func TestHelpPointsToCommandHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := App{Stdout: &stdout, Stderr: &stderr}
	if code := app.Run([]string{"help"}); code != Success {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	const hint = "Run '7331 <command> --help' for command-specific flags."
	if !strings.Contains(stdout.String(), hint) {
		t.Fatalf("help does not contain %q:\n%s", hint, stdout.String())
	}
}

func TestUploadOneAndFiveFiles(t *testing.T) {
	for _, count := range []int{1, 5} {
		t.Run(fmt.Sprintf("%d files", count), func(t *testing.T) {
			server, uploads := uploadServer(t, 0)
			defer server.Close()
			directory := t.TempDir()
			args := []string{"--server", server.URL, "upload", "--expires", "1h"}
			for index := 0; index < count; index++ {
				args = append(args, pngFile(t, directory, fmt.Sprintf("image-%d.png", index)))
			}
			app, stdout, stderr := testApp(server, filepath.Join(directory, "state"), false, false)
			if code := app.Run(args); code != Success {
				t.Fatalf("code = %d, stderr = %s", code, stderr.String())
			}
			if int(uploads.Load()) != count {
				t.Fatalf("uploads = %d, want %d", uploads.Load(), count)
			}
			if lines := strings.Count(strings.TrimSpace(stdout.String()), "\n") + 1; lines != count {
				t.Fatalf("output lines = %d: %s", lines, stdout.String())
			}
		})
	}
}

func TestUploadPromptsForDroppedFile(t *testing.T) {
	server, uploads := uploadServer(t, 0)
	defer server.Close()
	directory := t.TempDir()
	path := pngFile(t, directory, "image with spaces.png")
	app, stdout, stderr := testApp(server, filepath.Join(directory, "state"), false, true)
	app.Stdin = strings.NewReader(strings.ReplaceAll(path, " ", `\ `) + "\n")

	if code := app.Run([]string{"--server", server.URL, "upload", "--no-save"}); code != Success {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if uploads.Load() != 1 {
		t.Fatalf("uploads = %d, want 1", uploads.Load())
	}
	if !strings.Contains(stderr.String(), "Drag and drop one to five images here") {
		t.Fatalf("prompt missing from stderr: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "https://i.test/public_identifier_0001.png") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestUploadWithoutFilesDoesNotReadNoninteractiveStdin(t *testing.T) {
	server, _ := uploadServer(t, 0)
	defer server.Close()
	app, _, stderr := testApp(server, filepath.Join(t.TempDir(), "state"), false, false)
	if code := app.Run([]string{"--server", server.URL, "upload"}); code != InvalidInput {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "upload requires at least one file when stdin is noninteractive") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestParseDroppedPaths(t *testing.T) {
	paths, err := parseDroppedPaths(`/tmp/one\ image.png '/tmp/two image.jpg' "/tmp/three image.gif"`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/tmp/one image.png", "/tmp/two image.jpg", "/tmp/three image.gif"}
	if fmt.Sprint(paths) != fmt.Sprint(want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func TestUploadInteractiveNoSaveShowsDeletionURL(t *testing.T) {
	server, _ := uploadServer(t, 0)
	defer server.Close()
	directory := t.TempDir()
	path := pngFile(t, directory, "image.png")
	app, stdout, stderr := testApp(server, filepath.Join(directory, "state"), true, false)
	code := app.Run([]string{"--server=" + server.URL, "upload", path, "--no-save"})
	if code != Success {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Uploaded "+path) ||
		!strings.Contains(stdout.String(), "Delete   https://test/d/public_identifier_0001#token=secret-public_identifier_0001") {
		t.Fatalf("unexpected interactive output:\n%s", stdout.String())
	}
}

func TestUploadJSONAndPartialFailure(t *testing.T) {
	server, _ := uploadServer(t, 2)
	defer server.Close()
	directory := t.TempDir()
	first := pngFile(t, directory, "one.png")
	second := pngFile(t, directory, "two.png")
	app, stdout, stderr := testApp(server, filepath.Join(directory, "state"), false, false)
	code := app.Run([]string{"upload", first, "--json", second, "--server", server.URL})
	if code != Failure {
		t.Fatalf("code = %d, want 1", code)
	}
	var envelope struct {
		Version int `json:"version"`
		Uploads []struct {
			URL string `json:"url"`
		} `json:"uploads"`
		Errors []struct {
			Source    string `json:"source"`
			RequestID string `json:"request_id"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("json output: %v\n%s", err, stdout.String())
	}
	if envelope.Version != 1 || len(envelope.Uploads) != 1 || len(envelope.Errors) != 1 {
		t.Fatalf("unexpected envelope %#v", envelope)
	}
	if envelope.Errors[0].Source != second || envelope.Errors[0].RequestID != "req-fail" {
		t.Fatalf("unexpected error %#v", envelope.Errors[0])
	}
	if !strings.Contains(stderr.String(), "tickets paused (request req-fail)") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestInvalidFilesFailBeforeNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	app, _, _ := testApp(server, filepath.Join(t.TempDir(), "state"), false, false)
	if code := app.Run([]string{"--server", server.URL, "upload", "1", "2", "3", "4", "5", "6"}); code != InvalidInput {
		t.Fatalf("six-file code = %d", code)
	}
	large := filepath.Join(t.TempDir(), "large.png")
	handle, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Truncate(files.MaxBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = handle.Close()
	if code := app.Run([]string{"--server", server.URL, "upload", large}); code != InvalidInput {
		t.Fatalf("large-file code = %d", code)
	}
	if requests.Load() != 0 {
		t.Fatalf("network requests = %d", requests.Load())
	}
}

func TestTicketTimeoutIsActionable(t *testing.T) {
	originalTimeout := api.TicketTimeout
	api.TicketTimeout = 20 * time.Millisecond
	defer func() { api.TicketTimeout = originalTimeout }()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(writer, `{"data":{"ticket":"late"}}`)
	}))
	defer server.Close()
	directory := t.TempDir()
	path := pngFile(t, directory, "image.png")
	app, _, stderr := testApp(server, filepath.Join(directory, "state"), false, false)
	code := app.Run([]string{"--server", server.URL, "upload", path, "--no-save"})
	if code != Failure {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "context deadline exceeded") {
		t.Fatalf("timeout was not actionable: %s", stderr.String())
	}
}

func TestDeleteByIDAndURL(t *testing.T) {
	id := "public_identifier_0001"
	for _, test := range []struct {
		name string
		// value is built from the live test server so that the deletion URL
		// originates from the host the request is sent to.
		value func(serverURL string) string
		save  bool
	}{
		{"saved ID", func(string) string { return id }, true},
		{"deletion URL", func(serverURL string) string { return serverURL + "/d/" + id + "#token=url-secret" }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var gotPath, gotToken string
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				gotPath = request.URL.String()
				var body map[string]string
				_ = json.NewDecoder(request.Body).Decode(&body)
				gotToken = body["token"]
				fmt.Fprint(writer, `{"data":{"deleted":true}}`)
			}))
			defer server.Close()
			root := filepath.Join(t.TempDir(), "state")
			store, err := state.New(root)
			if err != nil {
				t.Fatal(err)
			}
			if test.save {
				if err := store.Save(state.Record{PublicID: id, DeletionToken: "saved-secret"}); err != nil {
					t.Fatal(err)
				}
			}
			app, stdout, stderr := testApp(server, root, false, false)
			code := app.Run([]string{"--server", server.URL, "delete", test.value(server.URL), "--yes", "--json"})
			if code != Success {
				t.Fatalf("code = %d, stderr = %s", code, stderr.String())
			}
			if strings.Contains(gotPath, "token") || strings.Contains(gotPath, "#") {
				t.Fatalf("fragment leaked into request URL: %s", gotPath)
			}
			wantToken := "url-secret"
			if test.save {
				wantToken = "saved-secret"
			}
			if gotToken != wantToken {
				t.Fatalf("token = %q, want %q", gotToken, wantToken)
			}
			if !strings.Contains(stdout.String(), `"deleted": true`) {
				t.Fatalf("stdout = %s", stdout.String())
			}
		})
	}
}

func TestDelete404RemovesRecordAnd403PreservesIt(t *testing.T) {
	id := "public_identifier_0001"
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/problem+json")
				writer.WriteHeader(status)
				fmt.Fprintf(writer, `{"status":%d,"detail":"result"}`, status)
			}))
			defer server.Close()
			root := filepath.Join(t.TempDir(), "state")
			store, err := state.New(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Save(state.Record{PublicID: id, DeletionToken: "secret"}); err != nil {
				t.Fatal(err)
			}
			app, _, _ := testApp(server, root, false, false)
			code := app.Run([]string{"--server", server.URL, "delete", id, "--yes"})
			_, loadErr := store.Load(id)
			if status == http.StatusNotFound {
				if code != Success || loadErr == nil {
					t.Fatalf("404: code=%d loadErr=%v", code, loadErr)
				}
			} else if code != Failure || loadErr != nil {
				t.Fatalf("403: code=%d loadErr=%v", code, loadErr)
			}
		})
	}
}

func TestDeleteNoninteractiveRequiresYes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		t.Fatal("network should not be used")
	}))
	defer server.Close()
	app, _, _ := testApp(server, filepath.Join(t.TempDir(), "state"), false, false)
	if code := app.Run([]string{"--server", server.URL, "delete", "public_identifier_0001"}); code != InvalidInput {
		t.Fatalf("code = %d", code)
	}
}

func TestDeleteRefusesForeignDeletionURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		t.Error("token must not be sent to a server that did not issue it")
	}))
	defer server.Close()
	app, _, stderr := testApp(server, filepath.Join(t.TempDir(), "state"), false, false)
	value := "https://elsewhere.example/d/public_identifier_0001#token=leaked"
	if code := app.Run([]string{"--server", server.URL, "delete", value, "--yes"}); code != InvalidInput {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "elsewhere.example") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestVersionFlagMatchesSubcommand(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		var stdout, stderr bytes.Buffer
		app := App{Stdout: &stdout, Stderr: &stderr, Version: "1.2.3"}
		if code := app.Run(args); code != Success {
			t.Fatalf("%v: code = %d, stderr = %s", args, code, stderr.String())
		}
		if strings.TrimSpace(stdout.String()) != "7331 1.2.3" {
			t.Fatalf("%v: stdout = %q", args, stdout.String())
		}
	}
}

func TestServerEnvironmentOverridePrefersShellSettableName(t *testing.T) {
	// A shell identifier cannot begin with a digit, so _7331_SERVER is the
	// name users can actually export.
	t.Setenv("_7331_SERVER", "https://underscore.example")
	t.Setenv("7331_SERVER", "https://digit.example")
	if got := serverFromEnvironment(); got != "https://underscore.example" {
		t.Fatalf("server = %q", got)
	}
	t.Setenv("_7331_SERVER", "")
	if got := serverFromEnvironment(); got != "https://digit.example" {
		t.Fatalf("fallback server = %q", got)
	}
}

func TestUploadRejectsFileLargerThanServerLimit(t *testing.T) {
	var uploads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/images" {
			uploads.Add(1)
			return
		}
		fmt.Fprint(writer, `{"data":{"ticket":"t","expires_in":300,"max_bytes":4}}`)
	}))
	defer server.Close()
	directory := t.TempDir()
	path := pngFile(t, directory, "image.png")
	app, _, stderr := testApp(server, filepath.Join(directory, "state"), false, false)
	if code := app.Run([]string{"--server", server.URL, "upload", path}); code != Failure {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if uploads.Load() != 0 {
		t.Fatalf("uploaded %d files past the server limit", uploads.Load())
	}
	if !strings.Contains(stderr.String(), "server accepts at most 4") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestUploadContinuesPastUnreadableFile(t *testing.T) {
	server, uploads := uploadServer(t, 0)
	defer server.Close()
	directory := t.TempDir()
	good := pngFile(t, directory, "good.png")
	missing := filepath.Join(directory, "absent.png")
	app, stdout, stderr := testApp(server, filepath.Join(directory, "state"), false, false)

	code := app.Run([]string{"--server", server.URL, "upload", missing, good})
	if code != Failure {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if uploads.Load() != 1 {
		t.Fatalf("uploads = %d, want the valid file to still be sent", uploads.Load())
	}
	if !strings.Contains(stdout.String(), "https://i.test/public_identifier_0001.png") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), missing) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestUploadWithOnlyInvalidFilesSkipsNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	directory := t.TempDir()
	app, _, stderr := testApp(server, filepath.Join(directory, "state"), false, false)
	code := app.Run([]string{"--server", server.URL, "upload", filepath.Join(directory, "absent.png")})
	if code != InvalidInput {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if requests.Load() != 0 {
		t.Fatalf("network requests = %d", requests.Load())
	}
}

func TestListShowsSavedUploadsAndHidesTokens(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	store, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	live := state.Record{
		PublicID:      "public_identifier_0001",
		DeletionToken: "must-not-be-listed",
		URL:           "https://i.test/live.png",
		DeletionURL:   "https://test/d/public_identifier_0001#token=must-not-be-listed",
		Filename:      "live.png",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		ExpiresAt:     time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	expired := state.Record{
		PublicID:      "public_identifier_0002",
		DeletionToken: "stale",
		URL:           "https://i.test/expired.png",
		Filename:      "expired.png",
		CreatedAt:     "2020-01-01T00:00:00Z",
		ExpiresAt:     "2020-01-02T00:00:00Z",
	}
	for _, record := range []state.Record{live, expired} {
		if err := store.Save(record); err != nil {
			t.Fatal(err)
		}
	}

	app, stdout, stderr := testApp(httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})), root, true, false)
	if code := app.Run([]string{"list"}); code != Success {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "live.png") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "must-not-be-listed") {
		t.Fatalf("list exposed a deletion token:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "expired.png") {
		t.Fatalf("list showed an expired upload:\n%s", stdout.String())
	}
	if _, err := store.Load(expired.PublicID); err == nil {
		t.Fatal("expired record was not pruned from disk")
	}
	if _, err := store.Load(live.PublicID); err != nil {
		t.Fatalf("live record was pruned: %v", err)
	}
}

func TestListJSONIncludesDeletionURLOnlyOnRequest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	store, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(state.Record{
		PublicID:      "public_identifier_0001",
		DeletionToken: "secret",
		URL:           "https://i.test/one.png",
		DeletionURL:   "https://test/d/public_identifier_0001#token=secret",
		ExpiresAt:     time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		args []string
		want bool
	}{
		{[]string{"list", "--json"}, false},
		{[]string{"list", "--json", "--show-delete-url"}, true},
	} {
		app, stdout, stderr := testApp(httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})), root, false, false)
		if code := app.Run(test.args); code != Success {
			t.Fatalf("%v: code = %d, stderr = %s", test.args, code, stderr.String())
		}
		var envelope output.ListEnvelope
		if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
			t.Fatalf("%v: %v\n%s", test.args, err, stdout.String())
		}
		if envelope.Version != 1 || len(envelope.Uploads) != 1 {
			t.Fatalf("%v: envelope = %#v", test.args, envelope)
		}
		if got := envelope.Uploads[0].DeletionURL != ""; got != test.want {
			t.Fatalf("%v: deletion URL present = %t, want %t", test.args, got, test.want)
		}
	}
}

func TestListWithoutRecordsSucceeds(t *testing.T) {
	app, stdout, stderr := testApp(httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})), filepath.Join(t.TempDir(), "state"), true, false)
	if code := app.Run([]string{"list"}); code != Success {
		t.Fatalf("code = %d", code)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want the empty listing on stderr", stdout.String())
	}
	if !strings.Contains(stderr.String(), "No saved uploads.") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

// A piped or --url-only run keeps stdout a clean URL list, so a deletion
// capability that was never saved locally has to survive on stderr.
func TestPipedUploadKeepsUnsavedDeletionURLReachable(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want bool
	}{
		{"piped and unsaved", []string{"--no-save"}, true},
		{"piped and requested", []string{"--show-delete-url"}, true},
		{"piped and saved", nil, false},
		{"url-only and unsaved", []string{"--url-only", "--no-save"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, _ := uploadServer(t, 0)
			defer server.Close()
			directory := t.TempDir()
			path := pngFile(t, directory, "image.png")
			app, stdout, stderr := testApp(server, filepath.Join(directory, "state"), false, false)

			args := append([]string{"--server", server.URL, "upload", path}, test.args...)
			if code := app.Run(args); code != Success {
				t.Fatalf("code = %d, stderr = %s", code, stderr.String())
			}
			const deletionURL = "https://test/d/public_identifier_0001#token=secret-public_identifier_0001"
			if strings.Contains(stdout.String(), "token") {
				t.Fatalf("deletion capability leaked into piped stdout:\n%s", stdout.String())
			}
			if got := strings.Contains(stderr.String(), deletionURL); got != test.want {
				t.Fatalf("deletion URL on stderr = %t, want %t\n%s", got, test.want, stderr.String())
			}
			if !strings.Contains(stdout.String(), "https://i.test/public_identifier_0001.png") {
				t.Fatalf("stdout = %s", stdout.String())
			}
		})
	}
}

func TestDeleteDeclinedStillEmitsJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("a declined deletion must not reach the network")
	}))
	defer server.Close()
	root := filepath.Join(t.TempDir(), "state")
	store, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(state.Record{PublicID: "public_identifier_0001", DeletionToken: "secret"}); err != nil {
		t.Fatal(err)
	}
	app, stdout, stderr := testApp(server, root, false, true)
	app.Stdin = strings.NewReader("n\n")

	if code := app.Run([]string{"--server", server.URL, "delete", "public_identifier_0001", "--json"}); code != Success {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	var result output.DeleteResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("declined deletion produced no JSON: %v\n%q", err, stdout.String())
	}
	if result.Deleted || result.PublicID != "public_identifier_0001" {
		t.Fatalf("result = %#v", result)
	}
}

// The host alone is not the origin: a credential issued over HTTPS must not be
// sent in cleartext to the same host.
func TestDeleteRefusesDowngradedDeletionURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("token must not be sent over a scheme that did not issue it")
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")
	app, _, stderr := testApp(server, filepath.Join(t.TempDir(), "state"), false, false)

	value := "https://" + host + "/d/public_identifier_0001#token=leaked"
	if code := app.Run([]string{"--server", server.URL, "delete", value, "--yes"}); code != InvalidInput {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "https://"+host) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

// A deletion URL is self-contained, so it has to work on a machine where the
// state directory cannot be created.
func TestDeleteByURLSurvivesUnusableStateDirectory(t *testing.T) {
	var deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		deleted = true
		fmt.Fprint(writer, `{"data":{"deleted":true}}`)
	}))
	defer server.Close()

	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	app, _, stderr := testApp(server, filepath.Join(blocked, "state"), false, false)
	value := server.URL + "/d/public_identifier_0001#token=url-secret"
	if code := app.Run([]string{"--server", server.URL, "delete", value, "--yes"}); code != Success {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if !deleted {
		t.Fatal("deletion URL was not used")
	}
	if !strings.Contains(stderr.String(), "continuing with the deletion URL") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestUsageOnFailureGoesToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := App{Stdout: &stdout, Stderr: &stderr}
	if code := app.Run(nil); code != InvalidInput {
		t.Fatalf("code = %d", code)
	}
	if stdout.String() != "" {
		t.Fatalf("failed run wrote usage to stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestSeparatorBeforeCommandIsAccepted(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := App{Stdout: &stdout, Stderr: &stderr, Version: "1.2.3"}
	if code := app.Run([]string{"--", "version"}); code != Success {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "7331 1.2.3" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestStateDirectoryEnvironmentOverride(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	store, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(state.Record{
		PublicID:      "public_identifier_0001",
		DeletionToken: "secret",
		URL:           "https://i.test/one.png",
		Filename:      "one.png",
		ExpiresAt:     time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("_7331_STATE_DIR", root)

	// StateRoot is left empty so that the environment is what resolves it.
	app, stdout, stderr := testApp(httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})), "", true, false)
	if code := app.Run([]string{"list"}); code != Success {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "one.png") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestExpiresErrorListsEveryChoice(t *testing.T) {
	app, _, stderr := testApp(httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})), t.TempDir(), false, false)
	if code := app.Run([]string{"upload", "--expires", "7h", "image.png"}); code != InvalidInput {
		t.Fatalf("code = %d", code)
	}
	for _, choice := range retentionChoices {
		if !strings.Contains(stderr.String(), choice.name) {
			t.Fatalf("%s missing from %q", choice.name, stderr.String())
		}
	}
	if !strings.Contains(stderr.String(), "or 24h") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

// A terminal that never sends a newline must not be read without bound.
func TestPromptReadsAreBounded(t *testing.T) {
	server, _ := uploadServer(t, 0)
	defer server.Close()
	app, _, stderr := testApp(server, filepath.Join(t.TempDir(), "state"), false, true)
	app.Stdin = endlessReader{}

	if code := app.Run([]string{"--server", server.URL, "upload"}); code != InvalidInput {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
}

// endlessReader models /dev/zero: a character device that never yields a line.
type endlessReader struct{}

func (endlessReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 'a'
	}
	return len(buffer), nil
}

func TestInfoAcceptsEveryReferenceForm(t *testing.T) {
	id := "public_identifier_0001"
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path != "/api/v1/public-images/"+id {
			t.Errorf("path = %s", request.URL.Path)
		}
		fmt.Fprintf(writer, `{"data":{"public_id":%q,"url":"https://i.test/image.png","thumbnail_url":"https://i.test/t.webp","mime_type":"image/png","size_bytes":8,"width":1,"height":1,"animated":false,"created_at":"now","expires_at":"later"}}`, id)
	}))
	defer server.Close()
	values := []string{
		id,
		"https://i.7331.cloud/" + id + ".png",
		"https://7331.cloud/f/" + id,
		"https://7331.cloud/d/" + id + "#token=must-not-be-exposed",
	}
	for _, value := range values {
		app, stdout, stderr := testApp(server, filepath.Join(t.TempDir(), "state"), false, false)
		if code := app.Run([]string{"--server", server.URL, "info", value, "--json"}); code != Success {
			t.Fatalf("%s: code=%d stderr=%s", value, code, stderr.String())
		}
		if strings.Contains(stdout.String(), "must-not-be-exposed") || strings.Contains(stdout.String(), "token") {
			t.Fatalf("info exposed deletion token: %s", stdout.String())
		}
	}
	if requests.Load() != int32(len(values)) {
		t.Fatalf("requests = %d", requests.Load())
	}
}
