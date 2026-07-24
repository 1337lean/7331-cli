package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/1337lean/7331-cli/internal/files"
)

type Client struct {
	base       *url.URL
	userAgent  string
	httpClient *http.Client
}

type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Code      string `json:"code"`
	Detail    string `json:"detail"`
	RequestID string `json:"request_id"`
}

type Error struct {
	Status  int
	Problem Problem
}

func (e *Error) Error() string {
	detail := e.Problem.Detail
	if detail == "" {
		detail = e.Problem.Title
	}
	if detail == "" {
		detail = http.StatusText(e.Status)
	}
	if e.Problem.RequestID != "" {
		return fmt.Sprintf("%s (request %s)", detail, e.Problem.RequestID)
	}
	return detail
}

type Ticket struct {
	Ticket    string `json:"ticket"`
	ExpiresIn int    `json:"expires_in"`
	MaxBytes  int64  `json:"max_bytes"`
}

type Upload struct {
	Source       string `json:"source,omitempty"`
	PublicID     string `json:"public_id"`
	URL          string `json:"url"`
	DetailsURL   string `json:"details_url"`
	ThumbnailURL string `json:"thumbnail_url"`
	DeletionURL  string `json:"deletion_url"`
	MIMEType     string `json:"mime_type"`
	SizeBytes    int64  `json:"size_bytes"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Animated     bool   `json:"animated"`
	CreatedAt    string `json:"created_at"`
	ExpiresAt    string `json:"expires_at"`
}

type Metadata struct {
	PublicID     string `json:"public_id"`
	URL          string `json:"url"`
	DetailsURL   string `json:"details_url,omitempty"`
	ThumbnailURL string `json:"thumbnail_url"`
	MIMEType     string `json:"mime_type"`
	SizeBytes    int64  `json:"size_bytes"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Animated     bool   `json:"animated"`
	CreatedAt    string `json:"created_at"`
	ExpiresAt    string `json:"expires_at"`
}

func New(server, version string, client *http.Client) (*Client, error) {
	base, err := url.Parse(server)
	if err != nil || base.Scheme == "" || base.Host == "" || base.User != nil {
		return nil, errors.New("server must be an absolute HTTP(S) URL")
	}
	if base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("server URL cannot contain a query or fragment")
	}
	if base.Scheme != "https" {
		host := base.Hostname()
		if base.Scheme != "http" || (!isLoopback(host) && !strings.EqualFold(host, "localhost")) {
			return nil, errors.New("server must use HTTPS except for loopback addresses")
		}
	}
	base.Path = strings.TrimRight(base.Path, "/")
	if client == nil {
		client = &http.Client{}
	}
	return &Client{
		base:       base,
		userAgent:  "7331/" + version,
		httpClient: client,
	}, nil
}

func isLoopback(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *Client) endpoint(parts ...string) string {
	copy := *c.base
	joined := append([]string{copy.Path}, parts...)
	copy.Path = path.Join(joined...)
	return copy.String()
}

func (c *Client) Ticket(ctx context.Context, file files.File, retentionSeconds int) (Ticket, error) {
	body, err := json.Marshal(map[string]any{
		"filename":          file.Filename,
		"size_bytes":        file.Size,
		"mime_type":         file.MIMEType,
		"retention_seconds": retentionSeconds,
	})
	if err != nil {
		return Ticket{}, err
	}
	var envelope struct {
		Data Ticket `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, c.endpoint("api", "v1", "cli", "upload-tickets"), bytes.NewReader(body), &envelope); err != nil {
		return Ticket{}, err
	}
	return envelope.Data, nil
}

func (c *Client) Upload(ctx context.Context, file files.File, ticket string) (Upload, error) {
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	writeDone := make(chan error, 1)
	go func() {
		defer close(writeDone)
		handle, err := os.Open(file.Path)
		if err == nil {
			var part io.Writer
			disposition := mime.FormatMediaType("form-data", map[string]string{
				"name":     "file",
				"filename": file.Filename,
			})
			part, err = multipartWriter.CreatePart(textproto.MIMEHeader{
				"Content-Disposition": {disposition},
				"Content-Type":        {file.MIMEType},
			})
			if err == nil {
				_, err = io.Copy(part, handle)
			}
			closeErr := handle.Close()
			if err == nil {
				err = closeErr
			}
		}
		if err == nil {
			err = multipartWriter.Close()
		}
		_ = writer.CloseWithError(err)
		writeDone <- err
	}()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("api", "v1", "images"), reader)
	if err != nil {
		_ = reader.CloseWithError(err)
		return Upload{}, err
	}
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	request.Header.Set("X-Upload-Ticket", ticket)
	request.Header.Set("User-Agent", c.userAgent)
	response, err := c.httpClient.Do(request)
	if err != nil {
		_ = reader.CloseWithError(err)
		<-writeDone
		return Upload{}, fmt.Errorf("upload request failed: %w", err)
	}
	defer response.Body.Close()
	_ = reader.Close()
	writeErr := <-writeDone
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Upload{}, decodeProblem(response)
	}
	if writeErr != nil {
		return Upload{}, fmt.Errorf("read upload file: %w", writeErr)
	}
	var envelope struct {
		Data Upload `json:"data"`
	}
	if err := decodeJSON(response.Body, &envelope); err != nil {
		return Upload{}, fmt.Errorf("decode upload response: %w", err)
	}
	envelope.Data.Source = file.Path
	return envelope.Data, nil
}

func (c *Client) Info(ctx context.Context, publicID string) (Metadata, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("api", "v1", "public-images", url.PathEscape(publicID)), nil)
	if err != nil {
		return Metadata{}, err
	}
	request.Header.Set("User-Agent", c.userAgent)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return Metadata{}, fmt.Errorf("info request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Metadata{}, decodeProblem(response)
	}
	var envelope struct {
		Data Metadata `json:"data"`
	}
	if err := decodeJSON(response.Body, &envelope); err != nil {
		return Metadata{}, fmt.Errorf("decode info response: %w", err)
	}
	envelope.Data.DetailsURL = strings.TrimRight(c.base.String(), "/") + "/f/" + publicID
	return envelope.Data, nil
}

func (c *Client) Delete(ctx context.Context, publicID, token string) (alreadyAbsent bool, err error) {
	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		return false, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint("api", "v1", "public-images", url.PathEscape(publicID)), bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", c.userAgent)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return false, fmt.Errorf("delete request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return true, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, decodeProblem(response)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return false, nil
}

func (c *Client) doJSON(ctx context.Context, method, endpoint string, body io.Reader, destination any) error {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", c.userAgent)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("ticket request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeProblem(response)
	}
	if err := decodeJSON(response.Body, destination); err != nil {
		return fmt.Errorf("decode ticket response: %w", err)
	}
	return nil
}

func decodeProblem(response *http.Response) error {
	var details Problem
	if err := decodeJSON(response.Body, &details); err != nil {
		return &Error{Status: response.StatusCode, Problem: Problem{
			Status: response.StatusCode,
			Title:  http.StatusText(response.StatusCode),
		}}
	}
	return &Error{Status: response.StatusCode, Problem: details}
}

func decodeJSON(reader io.Reader, destination any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, 1024*1024))
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	return nil
}

func IsStatus(err error, status int) bool {
	var apiError *Error
	return errors.As(err, &apiError) && apiError.Status == status
}

var TicketTimeout = 15 * time.Second
var UploadTimeout = 130 * time.Second
var MetadataTimeout = 15 * time.Second
