package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"
)

func Run(handler http.Handler, host string, port int, readTimeout, writeTimeout time.Duration, log *slog.Logger) error {
	addr := fmt.Sprintf("%s:%d", host, port)

	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  60 * time.Second,
	}

	done := make(chan error, 1)
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		<-sigCh
		log.Info("shutting down server")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		done <- srv.Shutdown(ctx)
	}()

	log.Info("server starting",
		"addr", addr,
		"url", fmt.Sprintf("http://%s", addr),
	)

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return <-done
}
