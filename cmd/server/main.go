package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MadAppGang/httplog"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var apiTimeout = 30 * time.Second
var readHeaderTimeout = 30 * time.Second
var injectedUploadDelay = 0 * time.Second

func main() {
	// zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	debug := flag.Bool("debug", false, "sets log level to debug")
	port := flag.Int("port", 8980, "port to run the server on")
	_apiTimeout := flag.Duration("api-timeout", 30*time.Second, "timeout for API calls")
	_readHeaderTimeout := flag.Duration("header-timeout", 30*time.Second, "timeout for reading headers")
	_injectedUploadDelay := flag.Duration("upload-delay", 0*time.Second, "injected upload delay every 32KB of data (10ms means around 3MBps)")

	flag.Parse()

	// Set the timeouts
	apiTimeout = *_apiTimeout
	readHeaderTimeout = *_readHeaderTimeout
	injectedUploadDelay = *_injectedUploadDelay

	// Default level for this example is info, unless debug flag is present
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	log.Debug().Msg("Debug mode is on")
	log.Debug().Dur("apiTimeout", apiTimeout).Dur("readHeaderTimeout", readHeaderTimeout).
		Dur("injectedUploadDelay", injectedUploadDelay).Msg("Timeouts set")

	serverPort := *port
	if err := run(serverPort); err != nil {
		log.Fatal().Err(err).Msg("fatal error during server run")
	}
}

func run(port int) error {
	// routing and handlers
	r := mux.NewRouter()
	r.Use(httplog.LoggerWithFormatter(httplog.DefaultLogFormatterWithResponseHeader))

	// Register pprof handlers
	r.HandleFunc("/debug/pprof/", pprof.Index)
	r.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	r.HandleFunc("/debug/pprof/profile", pprof.Profile)
	r.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	r.HandleFunc("/debug/pprof/trace", pprof.Trace)
	r.Handle("/debug/pprof/block", pprof.Handler("block"))
	r.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	r.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	r.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))

	r.HandleFunc("/upload/", uploadHandler).Methods("POST")
	// Add a timeout to the upload handler if desired at this level
	// r.Handle("/upload/", http.TimeoutHandler(http.HandlerFunc(uploadHandler), 10*time.Second, "timeout")).Methods("POST")

	apiRouter := r.PathPrefix("/").Subrouter()
	if apiTimeout > 0 {
		apiRouter.Use(newTimeoutMiddleware(apiTimeout))
	}
	apiRouter.HandleFunc("/", homeGETHandler).Methods("GET")
	apiRouter.HandleFunc("/", homePOSTHandlerWriteToFile).Methods("POST")

	// Create a custom server
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("could not listen on port %d: %v", port, err)
	}
	defer ln.Close()

	srv := &http.Server{
		Handler:           r,
		ReadTimeout:       0, // too limiting; implement ReadHeaderTimeout + in handler per use case
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      0, // too limiting; implement in handler per use case
		IdleTimeout:       1 * time.Minute,
	}

	srvErrCh := make(chan error, 1)
	go func() {
		log.Info().Int("port", port).Msg("Server starting")
		srvErrCh <- srv.Serve(ln)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	select {
	case <-quit:
	case err := <-srvErrCh:
		return fmt.Errorf("server start error: %v", err)
	}

	log.Debug().Msg("Shutting down server gracefully on interrupt signal")

	// graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("graceful server shutdown failed: %v", err)
	}

	log.Debug().Msg("Server Shutdown Completed Gracefully")
	return nil
}
