package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

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

func RequestID(err error) string {
	var apiError *api.Error
	if errors.As(err, &apiError) {
		return apiError.Problem.RequestID
	}
	return ""
}
