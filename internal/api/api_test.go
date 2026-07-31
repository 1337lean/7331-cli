package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1337lean/7331-cli/internal/files"
)

func TestUploadStreamsDeclaredFilenameAndMIME(t *testing.T) {
	t.Parallel()
	var gotFilename, gotMIME, gotUserAgent, gotTicket string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotUserAgent = request.Header.Get("User-Agent")
		gotTicket = request.Header.Get("X-Upload-Ticket")
		part, err := request.MultipartReader()
		if err != nil {
			t.Fatal(err)
		}
		filePart, err := part.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		gotFilename = filePart.FileName()
		gotMIME = filePart.Header.Get("Content-Type")
		if _, err := io.Copy(io.Discard, filePart); err != nil {
			t.Fatal(err)
		}
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprint(writer, `{"data":{"public_id":"abcdefghijklmnopqrst","url":"https://i.test/id.png"}}`)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "image.png")
	data := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := New(server.URL, "v0.1.0", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Upload(context.Background(), files.File{
		Path: path, Filename: "image.png", MIMEType: "image/png", Size: int64(len(data)),
	}, "one-use-ticket")
	if err != nil {
		t.Fatal(err)
	}
	if result.PublicID != "abcdefghijklmnopqrst" {
		t.Fatalf("unexpected result %#v", result)
	}
	if gotFilename != "image.png" || gotMIME != "image/png" {
		t.Fatalf("multipart declaration = %q %q", gotFilename, gotMIME)
	}
	if gotUserAgent != "7331/v0.1.0" || gotTicket != "one-use-ticket" {
		t.Fatalf("headers = %q %q", gotUserAgent, gotTicket)
	}
}

func TestProblemIncludesDetailAndRequestID(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/problem+json")
		writer.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(writer).Encode(Problem{
			Status: http.StatusServiceUnavailable, Detail: "try later", RequestID: "request-123",
		})
	}))
	defer server.Close()
	client, err := New(server.URL, "dev", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Ticket(context.Background(), files.File{
		Filename: "image.png", MIMEType: "image/png", Size: 8,
	}, 300)
	if err == nil || err.Error() != "try later (request request-123)" {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestNewRejectsInsecureNonLoopbackServer(t *testing.T) {
	t.Parallel()
	if _, err := New("http://example.com", "dev", nil); err == nil {
		t.Fatal("expected insecure server to fail")
	}
	if _, err := New("http://127.0.0.1:3000", "dev", nil); err != nil {
		t.Fatalf("loopback server should be accepted: %v", err)
	}
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()
	var destinationHit bool
	destination := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		destinationHit = true
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	client, err := New(source.URL, "dev", source.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Delete(context.Background(), "abcdefghijklmnopqrst", "deletion-secret")
	if !IsStatus(err, http.StatusTemporaryRedirect) {
		t.Fatalf("expected redirect response, got %v", err)
	}
	if destinationHit {
		t.Fatal("redirect destination received deletion capability")
	}
	// A redirect body is not a problem document, so the message has to be built
	// from the response itself rather than reported as an empty failure.
	if !strings.Contains(err.Error(), destination.URL) || !strings.Contains(err.Error(), "--server") {
		t.Fatalf("redirect error is not actionable: %v", err)
	}
}
