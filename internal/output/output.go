package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/1337lean/7331-cli/internal/api"
)

type UploadError struct {
	Source    string `json:"source"`
	Detail    string `json:"detail"`
	RequestID string `json:"request_id,omitempty"`
}

type UploadEnvelope struct {
	Version int           `json:"version"`
	Uploads []api.Upload  `json:"uploads"`
	Errors  []UploadError `json:"errors"`
}

// ListEntry omits the deletion token by default so that listing saved uploads
// is not itself a way to leak deletion capabilities.
type ListEntry struct {
	PublicID    string `json:"public_id"`
	Filename    string `json:"filename,omitempty"`
	URL         string `json:"url"`
	DetailsURL  string `json:"details_url,omitempty"`
	DeletionURL string `json:"deletion_url,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

type ListEnvelope struct {
	Version int         `json:"version"`
	Uploads []ListEntry `json:"uploads"`
}

type DeleteResult struct {
	Deleted       bool   `json:"deleted"`
	PublicID      string `json:"public_id"`
	AlreadyAbsent bool   `json:"already_absent"`
}

func JSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func UploadInteractive(writer io.Writer, upload api.Upload, showDeleteURL bool) {
	fmt.Fprintf(writer, "Uploaded %s\n", upload.Source)
	fmt.Fprintf(writer, "URL      %s\n", upload.URL)
	fmt.Fprintf(writer, "Details  %s\n", upload.DetailsURL)
	fmt.Fprintf(writer, "Expires  %s\n", upload.ExpiresAt)
	if showDeleteURL {
		fmt.Fprintf(writer, "Delete   %s\n", upload.DeletionURL)
	} else {
		fmt.Fprintf(writer, "Delete   7331 delete %s\n", upload.PublicID)
	}
}

func InfoInteractive(writer io.Writer, metadata api.Metadata) {
	fmt.Fprintf(writer, "ID       %s\n", metadata.PublicID)
	fmt.Fprintf(writer, "URL      %s\n", metadata.URL)
	fmt.Fprintf(writer, "Details  %s\n", metadata.DetailsURL)
	fmt.Fprintf(writer, "Type     %s\n", metadata.MIMEType)
	fmt.Fprintf(writer, "Size     %d bytes\n", metadata.SizeBytes)
	fmt.Fprintf(writer, "Image    %d×%d\n", metadata.Width, metadata.Height)
	fmt.Fprintf(writer, "Animated %t\n", metadata.Animated)
	fmt.Fprintf(writer, "Created  %s\n", metadata.CreatedAt)
	fmt.Fprintf(writer, "Expires  %s\n", metadata.ExpiresAt)
}

func ListInteractive(writer io.Writer, entries []ListEntry) {
	table := tabwriter.NewWriter(writer, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "PUBLIC ID\tFILENAME\tEXPIRES\tURL")
	for _, entry := range entries {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", entry.PublicID, dash(entry.Filename), dash(entry.ExpiresAt), entry.URL)
		if entry.DeletionURL != "" {
			fmt.Fprintf(table, "\t\t\t%s\n", entry.DeletionURL)
		}
	}
	_ = table.Flush()
}

func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func RequestID(err error) string {
	var apiError *api.Error
	if errors.As(err, &apiError) {
		return apiError.Problem.RequestID
	}
	return ""
}
