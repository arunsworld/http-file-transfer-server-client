package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

func homeGETHandler(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprint(w, "OK")
}

func homePOSTHandler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		log.Debug().Msg("homePOSTHandler finished")
	}()
	log.Debug().Msg("homePOSTHandler called")
	// Read the body
	var content bytes.Buffer

	_, err := io.CopyBuffer(&content, newSlowReader(r.Body, injectedUploadDelay), make([]byte, 32*1024))
	if err != nil {
		log.Error().Err(err).Msg("In homePOSTHandler: error reading body")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	log.Debug().Int("contentLength", content.Len()).Msg("homePOSTHandler: Body read successfully")

	fmt.Fprintf(w, "Received %d bytes: %s\n\n\n", content.Len(), content.String())
}

func homePOSTHandlerWriteToFile(w http.ResponseWriter, r *http.Request) {
	defer func() {
		log.Debug().Msg("homePOSTHandler finished")
	}()
	log.Debug().Msg("homePOSTHandler called")
	// Read the body

	f, err := os.Create(fixedFileLocation)
	if err != nil {
		log.Error().Err(err).Msg("In homePOSTHandler: error creatinf file")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	_, err = io.Copy(f, newSlowReader(r.Body, injectedUploadDelay))
	if err != nil {
		log.Error().Err(err).Msg("In homePOSTHandler: error reading body")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	fmt.Fprintln(w, "DONE")
}

var fixedFileLocation = "/tmp/uploaded-file"

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		log.Debug().Msg("Upload handler finished")
	}()
	log.Debug().Msg("Upload handler called")
	// Check for multipart/form-data
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		log.Error().Msgf("Unsupported Media Type: %s", r.Header.Get("Content-Type"))
		http.Error(w, "Unsupported Media Type", http.StatusUnsupportedMediaType)
		return
	}

	// Create a multipart reader
	mr, err := r.MultipartReader()
	if err != nil {
		log.Error().Err(err).Msg("Error creating multipart reader")
		http.Error(w, "Error parsing request", http.StatusBadRequest)
		return
	}

	uploadedFileName := ""
	attributes := map[string]string{}

	// Process each part
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break // End of parts
		}
		if err != nil {
			log.Error().Err(err).Msg("Error reading next part")
			http.Error(w, "Error parsing request (next part)", http.StatusBadRequest)
			return
		}

		// Handle different parts based on Content-Disposition
		switch part.FormName() {
		case "object":
			log.Debug().Msg("Processing object")
			filename := part.FileName()
			if filename == "" {
				log.Error().Msg("uploadHandler: File name not provided")
				http.Error(w, "Cannot process object without filename", http.StatusBadRequest)
				return
			}
			attributes["filename"] = filename

			fname, err := uploadStreamerToFixedLocation(newSlowReader(part, injectedUploadDelay))
			if err != nil {
				log.Error().Err(err).Msg("Error saving file")
				http.Error(w, "Error saving file", http.StatusInternalServerError)
				return
			}
			uploadedFileName = fname

			fmt.Fprintf(w, "File '%s' uploaded successfully\n", filename)
		default:
			attributeName := part.FormName()
			log.Debug().Str("Attribute", attributeName).Msg("Processing attribute")
			buf := &bytes.Buffer{}
			_, err := io.Copy(buf, part)
			if err != nil {
				log.Error().Err(err).Msgf("Error reading %s", attributeName)
				http.Error(w, "Error reading attribute", http.StatusBadRequest)
				return
			}
			data := buf.String()
			attributes[attributeName] = data
		}
	}

	if uploadedFileName == "" {
		log.Error().Msg("uploadHandler: No file uploaded")
		http.Error(w, "No file uploaded", http.StatusBadRequest)
		return
	}

	log.Info().Any("Attributes", attributes).Str("fileDestinationOnServer", uploadedFileName).Msg("File uploaded successfully")

	w.WriteHeader(http.StatusOK)
}

func uploadStreamerToTempLocation(r io.Reader) (string, error) {
	// Create a temporary file
	f, err := os.CreateTemp("", "upload-")
	if err != nil {
		return "", fmt.Errorf("error creating file: %w", err)
	}
	defer f.Close()

	// Copy reader data to file
	_, err = io.Copy(f, r)
	if err != nil {
		return "", fmt.Errorf("error copying file data: %w", err)
	}

	return f.Name(), nil
}

func uploadStreamerToFixedLocation(r io.Reader) (string, error) {
	f, err := os.Create(fixedFileLocation)
	if err != nil {
		return "", fmt.Errorf("error creating file: %w", err)
	}
	defer f.Close()

	// Copy reader data to file
	_, err = io.Copy(f, r)
	if err != nil {
		return "", fmt.Errorf("error copying file data: %w", err)
	}

	return fixedFileLocation, nil
}

func newSlowReader(r io.Reader, delay time.Duration) io.Reader {
	return &slowReader{wrappedReader: r, delay: delay}
}

type slowReader struct {
	wrappedReader io.Reader
	dataBuffer    int
	delay         time.Duration
}

func (s *slowReader) Read(p []byte) (n int, err error) {
	if s.delay > 0 && s.dataBuffer >= 32_000 {
		time.Sleep(s.delay)
		s.dataBuffer = 0
	}
	n, err = s.wrappedReader.Read(p)
	s.dataBuffer += n
	// log.Debug().Int("dataBuffer", s.dataBuffer).Int("n", n).Int("len(p)", len(p)).Msg("slowReader")
	return
}

func newTimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, timeout, "Request timed out")
	}
}
