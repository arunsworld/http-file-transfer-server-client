package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/arunsworld/nursery"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/schollz/progressbar/v3"
)

func main() {
	// zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	debug := flag.Bool("debug", false, "sets log level to debug")
	endpoint := flag.String("endpoint", "http://localhost:8980/upload/", "endpoint to upload file to")
	filename := flag.String("filename", "", "file to upload")

	flag.Parse()

	// Default level for this example is info, unless debug flag is present
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	log.Debug().Msg("Debug mode is on")

	if err := run(*endpoint, *filename); err != nil {
		log.Fatal().Err(err).Msg("fatal error during client run")
	}
}

func run(endpoint string, filename string) error {
	if filename == "" {
		return fmt.Errorf("filename is required")
	}

	log.Debug().Msgf("Uploading %s to %s", filename, endpoint)
	defer func() {
		log.Debug().Msgf("Finished uploading %s to %s", filename, endpoint)
	}()

	client := &http.Client{}

	attributes := map[string]string{
		"primaryKey": "abcdef",
		"version":    "1",
		"timestamp":  "2021-07-01T12:00:00Z",
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	return uploadFileStreaming(ctx, client, filename, attributes, endpoint)
}

func uploadFileStreaming(ctx context.Context, client *http.Client, filename string, attributes map[string]string, url string) error {
	fileInfo, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("failed to get file info for %s: %w", filename, err)
	}
	sizeInBytes := fileInfo.Size()
	log.Debug().Int64("sizeInBytes", sizeInBytes).Str("filename", filename).Msg("Upload file size")

	bar := progressbar.DefaultBytes(
		sizeInBytes,
		"uploading",
	)

	r, w := io.Pipe()
	writer := multipart.NewWriter(w)

	req, err := http.NewRequest("POST", url, r)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	return nursery.RunConcurrently(
		// Write the multipart form to the pipe
		func(_ context.Context, errCh chan error) {
			defer w.Close()
			// Add attributes to the multipart form
			for key, value := range attributes {
				err := writer.WriteField(key, value)
				if err != nil {
					log.Error().Err(err).Msg("Error writing field")
					errCh <- fmt.Errorf("failed to write field: %w", err)
					w.CloseWithError(fmt.Errorf("failed to write field: %w", err))
					return
				}
			}

			part, err := writer.CreateFormFile("object", filepath.Base(filename))
			if err != nil {
				log.Error().Err(err).Msg("Error creating form file")
				errCh <- fmt.Errorf("failed to create form file: %w", err)
				w.CloseWithError(fmt.Errorf("failed to create form file: %w", err))
				return
			}
			log.Debug().Msg("Created form file")

			file, err := os.Open(filename)
			if err != nil {
				log.Error().Err(err).Str("filename", filename).Msg("Error opening file")
				errCh <- fmt.Errorf("failed to open %s: %w", filename, err)
				w.CloseWithError(fmt.Errorf("failed to open %s: %w", filename, err))
				return
			}
			defer file.Close()

			progression := make(chan int, 1)

			if err := nursery.RunConcurrently(
				// Write the file data to the part
				func(_ context.Context, errCh chan error) {
					defer close(progression)
					log.Debug().Msg("Copying file data")
					if written, err := io.Copy(part, newReaderWithProgress(progression, newCtxAwareReader(ctx, file))); err != nil {
						errCh <- err
					} else {
						log.Debug().Int64("written", written).Msg("Copied file data")
					}
				},
				// Track progression
				func(context.Context, chan error) {
					for p := range progression {
						bar.Add(p)
					}
				},
			); err != nil {
				log.Error().Err(err).Msg("Error copying file data")
				errCh <- fmt.Errorf("failed to copy file data: %w", err)
				w.CloseWithError(fmt.Errorf("failed to copy file data: %w", err))
				return
			}

			if err := writer.Close(); err != nil {
				log.Error().Err(err).Msg("Error closing writer")
				errCh <- fmt.Errorf("failed to close writer: %w", err)
				w.CloseWithError(fmt.Errorf("failed to close writer: %w", err))
				return
			}

		},
		// Send the request
		func(_ context.Context, errCh chan error) {
			log.Debug().Msg("Sending request")
			resp, err := client.Do(req)
			if err != nil {
				log.Error().Err(err).Msg("Error sending request")
				errCh <- fmt.Errorf("failed to send request: %w", err)
				return
			}
			defer resp.Body.Close()

			log.Debug().Msg("Request sent successfully")

			respContent := &bytes.Buffer{}
			_, _ = io.Copy(respContent, resp.Body)

			// log.Debug().Int("StatusCode", resp.StatusCode).Str("response", respContent.String()).Msg("Response received")

			// Check response status
			if resp.StatusCode != http.StatusOK {
				log.Error().Msgf("Server returned status code: %d and content: %s", resp.StatusCode, respContent.String())
				errCh <- fmt.Errorf("server returned status code: %d [%s]", resp.StatusCode, respContent.String())
			}
		},
	)
}

func newCtxAwareReader(ctx context.Context, r io.Reader) io.Reader {
	return &ctxAwareReader{
		ctx:           ctx,
		wrappedReader: r,
	}
}

type ctxAwareReader struct {
	ctx           context.Context
	wrappedReader io.Reader
}

func (r *ctxAwareReader) Read(p []byte) (n int, err error) {
	select {
	case <-r.ctx.Done():
		return 0, fmt.Errorf("context cancelled")
	default:
		return r.wrappedReader.Read(p)
	}
}

func newReaderWithProgress(progress chan<- int, r io.Reader) io.Reader {
	return &readerWithProgress{
		progress:      progress,
		wrappedReader: r,
	}
}

type readerWithProgress struct {
	progress      chan<- int
	wrappedReader io.Reader
}

func (r *readerWithProgress) Read(p []byte) (n int, err error) {
	n, err = r.wrappedReader.Read(p)
	r.progress <- n
	return
}
