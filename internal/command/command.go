package command

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/1337lean/7331-cli/internal/api"
	"github.com/1337lean/7331-cli/internal/files"
	"github.com/1337lean/7331-cli/internal/output"
	"github.com/1337lean/7331-cli/internal/state"
)

const (
	Success      = 0
	Failure      = 1
	InvalidInput = 2
)

type App struct {
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	StdinTTY   bool
	StdoutTTY  bool
	HTTPClient *http.Client
	StateRoot  string
	Version    string
	Commit     string
	BuildDate  string
}

func (app App) Run(args []string) int {
	server, remaining, err := globalServer(args)
	if err != nil {
		return app.invalid(err)
	}
	if server == "" {
		server = os.Getenv("7331_SERVER")
	}
	if server == "" {
		server = "https://7331.cloud"
	}
	if len(remaining) == 0 {
		app.printHelp()
		return InvalidInput
	}
	switch remaining[0] {
	case "help", "--help", "-h":
		app.printHelp()
		return Success
	case "version":
		return app.runVersion(remaining[1:])
	case "upload":
		return app.runUpload(server, remaining[1:])
	case "delete":
		return app.runDelete(server, remaining[1:])
	case "info":
		return app.runInfo(server, remaining[1:])
	default:
		return app.invalid(fmt.Errorf("unknown command %q", remaining[0]))
	}
}

func globalServer(args []string) (string, []string, error) {
	var server string
	remaining := make([]string, 0, len(args))
	flags := true
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if flags && arg == "--" {
			flags = false
			remaining = append(remaining, arg)
			continue
		}
		if flags && arg == "--server" {
			if index+1 >= len(args) {
				return "", nil, errors.New("--server requires a URL")
			}
			index++
			server = args[index]
			continue
		}
		if flags && strings.HasPrefix(arg, "--server=") {
			server = strings.TrimPrefix(arg, "--server=")
			if server == "" {
				return "", nil, errors.New("--server requires a URL")
			}
			continue
		}
		remaining = append(remaining, arg)
	}
	return server, remaining, nil
}

type uploadOptions struct {
	paths         []string
	retention     string
	json          bool
	urlOnly       bool
	showDeleteURL bool
	noSave        bool
	help          bool
}

func parseUpload(args []string) (uploadOptions, error) {
	options := uploadOptions{retention: "24h"}
	positional := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if !positional && arg == "--" {
			positional = true
			continue
		}
		if !positional {
			switch {
			case arg == "--expires":
				if index+1 >= len(args) {
					return options, errors.New("--expires requires a duration")
				}
				index++
				options.retention = args[index]
				continue
			case strings.HasPrefix(arg, "--expires="):
				options.retention = strings.TrimPrefix(arg, "--expires=")
				continue
			case arg == "--json":
				options.json = true
				continue
			case arg == "--url-only":
				options.urlOnly = true
				continue
			case arg == "--show-delete-url":
				options.showDeleteURL = true
				continue
			case arg == "--no-save":
				options.noSave = true
				continue
			case arg == "--help" || arg == "-h":
				options.help = true
				continue
			case strings.HasPrefix(arg, "-"):
				return options, fmt.Errorf("unknown upload flag %q", arg)
			}
		}
		options.paths = append(options.paths, arg)
	}
	if options.json && options.urlOnly {
		return options, errors.New("--json and --url-only cannot be used together")
	}
	return options, nil
}

var retentionSeconds = map[string]int{
	"5m": 300, "10m": 600, "30m": 1800, "1h": 3600,
	"6h": 21600, "12h": 43200, "24h": 86400,
}

func (app App) runUpload(server string, args []string) int {
	options, err := parseUpload(args)
	if err != nil {
		return app.invalid(err)
	}
	if options.help {
		app.printUploadHelp()
		return Success
	}
	retention, ok := retentionSeconds[options.retention]
	if !ok {
		return app.invalid(errors.New("--expires must be one of 5m, 10m, 30m, 1h, 6h, 12h, or 24h"))
	}
	validated, err := files.Validate(options.paths)
	if err != nil {
		return app.invalid(err)
	}
	defer func() {
		for index := range validated {
			_ = validated[index].Close()
		}
	}()
	client, err := api.New(server, app.Version, app.HTTPClient)
	if err != nil {
		return app.invalid(err)
	}
	var store *state.Store
	if !options.noSave {
		store, err = state.New(app.StateRoot)
		if err != nil {
			fmt.Fprintf(app.Stderr, "7331: %v\n", err)
			return Failure
		}
	}

	uploads := make([]api.Upload, 0, len(validated))
	failures := make([]output.UploadError, 0)
	for _, file := range validated {
		ticketContext, cancelTicket := context.WithTimeout(context.Background(), api.TicketTimeout)
		ticket, ticketErr := client.Ticket(ticketContext, file, retention)
		cancelTicket()
		if ticketErr != nil {
			app.uploadFailure(file.Path, ticketErr, &failures)
			continue
		}
		uploadContext, cancelUpload := context.WithTimeout(context.Background(), api.UploadTimeout)
		uploaded, uploadErr := client.Upload(uploadContext, file, ticket.Ticket)
		cancelUpload()
		if uploadErr != nil {
			app.uploadFailure(file.Path, uploadErr, &failures)
			continue
		}
		uploaded.Source = file.Path
		uploads = append(uploads, uploaded)

		if store != nil {
			token, tokenErr := state.DeletionToken(uploaded.DeletionURL)
			if tokenErr == nil {
				tokenErr = store.Save(state.Record{
					PublicID:      uploaded.PublicID,
					DeletionToken: token,
					URL:           uploaded.URL,
					DetailsURL:    uploaded.DetailsURL,
					DeletionURL:   uploaded.DeletionURL,
					Filename:      file.Filename,
					CreatedAt:     uploaded.CreatedAt,
					ExpiresAt:     uploaded.ExpiresAt,
				})
			}
			if tokenErr != nil {
				app.uploadFailure(file.Path, fmt.Errorf("save deletion credential: %w", tokenErr), &failures)
			}
		}
	}

	if options.json {
		if err := output.JSON(app.Stdout, output.UploadEnvelope{
			Version: 1,
			Uploads: uploads,
			Errors:  failures,
		}); err != nil {
			fmt.Fprintf(app.Stderr, "7331: write output: %v\n", err)
			return Failure
		}
	} else if options.urlOnly || !app.StdoutTTY {
		for _, upload := range uploads {
			fmt.Fprintln(app.Stdout, upload.URL)
		}
	} else {
		for index, upload := range uploads {
			if index > 0 {
				fmt.Fprintln(app.Stdout)
			}
			output.UploadInteractive(app.Stdout, upload, options.showDeleteURL || options.noSave)
		}
	}
	if len(failures) > 0 {
		return Failure
	}
	return Success
}

func (app App) uploadFailure(source string, err error, failures *[]output.UploadError) {
	fmt.Fprintf(app.Stderr, "7331: %s: %v\n", source, err)
	*failures = append(*failures, output.UploadError{
		Source:    source,
		Detail:    err.Error(),
		RequestID: output.RequestID(err),
	})
}

type deleteOptions struct {
	value string
	yes   bool
	json  bool
	help  bool
}

func parseDelete(args []string) (deleteOptions, error) {
	var options deleteOptions
	positional := false
	for _, arg := range args {
		if !positional && arg == "--" {
			positional = true
			continue
		}
		if !positional {
			switch arg {
			case "--yes", "-y":
				options.yes = true
				continue
			case "--json":
				options.json = true
				continue
			case "--help", "-h":
				options.help = true
				continue
			}
			if strings.HasPrefix(arg, "-") {
				return options, fmt.Errorf("unknown delete flag %q", arg)
			}
		}
		if options.value != "" {
			return options, errors.New("delete accepts exactly one public ID or deletion URL")
		}
		options.value = arg
	}
	return options, nil
}

func (app App) runDelete(server string, args []string) int {
	options, err := parseDelete(args)
	if err != nil {
		return app.invalid(err)
	}
	if options.help {
		app.printDeleteHelp()
		return Success
	}
	if options.value == "" {
		return app.invalid(errors.New("delete requires a public ID or deletion URL"))
	}
	if !options.yes && !app.StdinTTY {
		return app.invalid(errors.New("delete requires --yes when stdin is noninteractive"))
	}
	reference, err := state.ParseReference(options.value)
	if err != nil {
		return app.invalid(err)
	}
	store, err := state.New(app.StateRoot)
	if err != nil {
		fmt.Fprintf(app.Stderr, "7331: %v\n", err)
		return Failure
	}
	token := reference.Token
	if token == "" {
		record, loadErr := store.Load(reference.PublicID)
		if loadErr != nil {
			return app.invalid(loadErr)
		}
		token = record.DeletionToken
	}
	if !options.yes {
		fmt.Fprintf(app.Stderr, "Delete %s? [y/N] ", reference.PublicID)
		answer, readErr := bufio.NewReader(app.Stdin).ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			fmt.Fprintf(app.Stderr, "7331: read confirmation: %v\n", readErr)
			return Failure
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(app.Stderr, "Deletion cancelled.")
			return Success
		}
	}
	client, err := api.New(server, app.Version, app.HTTPClient)
	if err != nil {
		return app.invalid(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), api.MetadataTimeout)
	alreadyAbsent, err := client.Delete(ctx, reference.PublicID, token)
	cancel()
	if err != nil {
		fmt.Fprintf(app.Stderr, "7331: %v\n", err)
		return Failure
	}
	if err := store.Remove(reference.PublicID); err != nil {
		fmt.Fprintf(app.Stderr, "7331: remove local deletion credential: %v\n", err)
		return Failure
	}
	result := output.DeleteResult{
		Deleted:       true,
		PublicID:      reference.PublicID,
		AlreadyAbsent: alreadyAbsent,
	}
	if options.json {
		if err := output.JSON(app.Stdout, result); err != nil {
			fmt.Fprintf(app.Stderr, "7331: write output: %v\n", err)
			return Failure
		}
	} else if alreadyAbsent {
		fmt.Fprintf(app.Stdout, "%s is already absent.\n", reference.PublicID)
	} else {
		fmt.Fprintf(app.Stdout, "Deleted %s.\n", reference.PublicID)
	}
	return Success
}

type infoOptions struct {
	value string
	json  bool
	help  bool
}

func parseInfo(args []string) (infoOptions, error) {
	var options infoOptions
	positional := false
	for _, arg := range args {
		if !positional && arg == "--" {
			positional = true
			continue
		}
		if !positional {
			switch arg {
			case "--json":
				options.json = true
				continue
			case "--help", "-h":
				options.help = true
				continue
			}
			if strings.HasPrefix(arg, "-") {
				return options, fmt.Errorf("unknown info flag %q", arg)
			}
		}
		if options.value != "" {
			return options, errors.New("info accepts exactly one public ID or URL")
		}
		options.value = arg
	}
	return options, nil
}

func (app App) runInfo(server string, args []string) int {
	options, err := parseInfo(args)
	if err != nil {
		return app.invalid(err)
	}
	if options.help {
		app.printInfoHelp()
		return Success
	}
	if options.value == "" {
		return app.invalid(errors.New("info requires a public ID or URL"))
	}
	reference, err := state.ParseReference(options.value)
	if err != nil {
		return app.invalid(err)
	}
	client, err := api.New(server, app.Version, app.HTTPClient)
	if err != nil {
		return app.invalid(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), api.MetadataTimeout)
	metadata, err := client.Info(ctx, reference.PublicID)
	cancel()
	if err != nil {
		fmt.Fprintf(app.Stderr, "7331: %v\n", err)
		return Failure
	}
	if options.json {
		if err := output.JSON(app.Stdout, metadata); err != nil {
			fmt.Fprintf(app.Stderr, "7331: write output: %v\n", err)
			return Failure
		}
	} else {
		output.InfoInteractive(app.Stdout, metadata)
	}
	return Success
}

func (app App) runVersion(args []string) int {
	if len(args) > 0 {
		if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
			fmt.Fprintln(app.Stdout, "Usage: 7331 version")
			return Success
		}
		return app.invalid(errors.New("version does not accept arguments"))
	}
	fmt.Fprintf(app.Stdout, "7331 %s", app.Version)
	if app.Commit != "" || app.BuildDate != "" {
		fmt.Fprintf(app.Stdout, " (commit %s, built %s)", fallback(app.Commit), fallback(app.BuildDate))
	}
	fmt.Fprintln(app.Stdout)
	return Success
}

func fallback(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func (app App) invalid(err error) int {
	fmt.Fprintf(app.Stderr, "7331: %v\n", err)
	fmt.Fprintln(app.Stderr, "Run '7331 help' for usage.")
	return InvalidInput
}

func (app App) printHelp() {
	fmt.Fprint(app.Stdout, `7331 uploads and manages anonymous images on 7331.cloud.

Usage:
  7331 [--server URL] upload FILE... [flags]
  7331 [--server URL] delete PUBLIC_ID|DELETION_URL [--yes] [--json]
  7331 [--server URL] info PUBLIC_ID|URL [--json]
  7331 version
  7331 help

Commands:
  upload   Upload one to five JPEG, PNG, WebP, or GIF images
  delete   Delete an upload using a saved credential or deletion URL
  info     Fetch public image metadata
  version  Print version information

Run '7331 <command> --help' for command-specific flags.
`)
}

func (app App) printUploadHelp() {
	fmt.Fprint(app.Stdout, `Usage: 7331 upload FILE... [flags]

Flags:
  --expires DURATION    5m, 10m, 30m, 1h, 6h, 12h, or 24h (default 24h)
  --json                Print a stable JSON envelope
  --url-only            Print direct URLs only
  --show-delete-url     Print the deletion capability URL
  --no-save             Do not save deletion credentials locally
`)
}

func (app App) printDeleteHelp() {
	fmt.Fprint(app.Stdout, `Usage: 7331 delete PUBLIC_ID|DELETION_URL [flags]

Flags:
  --yes, -y  Skip interactive confirmation
  --json     Print a JSON result
`)
}

func (app App) printInfoHelp() {
	fmt.Fprint(app.Stdout, `Usage: 7331 info PUBLIC_ID|URL [--json]
`)
}
