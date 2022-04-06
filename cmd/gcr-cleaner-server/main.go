// Copyright 2019 The GCR Cleaner Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package main defines the server interface for GCR Cleaner.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	gcrauthn "github.com/google/go-containerregistry/pkg/authn"
	gcrgoogle "github.com/google/go-containerregistry/pkg/v1/google"
	"github.com/sethvargo/gcr-cleaner/pkg/gcrcleaner"
)

var (
	stdout = os.Stdout
	stderr = os.Stderr
)

var (
	logLevel = os.Getenv("GCRCLEANER_LOG")
)

func main() {
	logger := gcrcleaner.NewLogger(logLevel, stderr, stdout)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := realMain(ctx, logger); err != nil {
		cancel()
		logger.Fatal("server exited with error", "error", err)
	}

	logger.Info("server shutdown complete")
}

func realMain(ctx context.Context, logger *gcrcleaner.Logger) error {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port

	var auther gcrauthn.Authenticator
	if token := os.Getenv("GCRCLEANER_TOKEN"); token != "" {
		logger.Debug("using token from GCRCLEANER_TOKEN for authentication")
		auther = &gcrauthn.Bearer{Token: token}
	} else {
		logger.Debug("using default token resolution for authentication")
		var err error
		auther, err = gcrgoogle.NewEnvAuthenticator()
		if err != nil {
			return fmt.Errorf("failed to setup auther: %w", err)
		}
	}

	concurrency := runtime.NumCPU()
	cleaner, err := gcrcleaner.NewCleaner(auther, logger, concurrency)
	if err != nil {
		return fmt.Errorf("failed to create cleaner: %w", err)
	}

	cleanerServer, err := gcrcleaner.NewServer(cleaner)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	cache := gcrcleaner.NewTimerCache(5 * time.Minute)

	mux := http.NewServeMux()
	mux.Handle("/http", cleanerServer.HTTPHandler())
	mux.Handle("/pubsub", cleanerServer.PubSubHandler(cache))

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server is listening", "port", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			select {
			case errCh <- err:
			default:
			}
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		return fmt.Errorf("server exited: %w", err)
	}

	logger.Info("server received stop, shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}

	return nil
}
