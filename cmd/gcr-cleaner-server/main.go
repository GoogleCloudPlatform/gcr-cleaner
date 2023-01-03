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
	"strconv"
	"syscall"
	"time"

	"github.com/GoogleCloudPlatform/gcr-cleaner/internal/bearerkeychain"
	"github.com/GoogleCloudPlatform/gcr-cleaner/internal/version"
	"github.com/GoogleCloudPlatform/gcr-cleaner/pkg/gcrcleaner"
	gcrauthn "github.com/google/go-containerregistry/pkg/authn"
	gcrgoogle "github.com/google/go-containerregistry/pkg/v1/google"
)

var (
	stdout = os.Stdout
	stderr = os.Stderr
)

var (
	logLevel    = os.Getenv("GCRCLEANER_LOG")
	concurrency = func() int64 {
		v := os.Getenv("GCRCLEANER_CONCURRENCY")
		if v == "" {
			return 20
		}

		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			panic(fmt.Errorf("failed to parse concurrency: %w", err))
		}
		return i
	}()
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
	logger.Debug("server is starting", "version", version.HumanVersion)
	defer logger.Debug("server finished")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port

	keychain := gcrauthn.NewMultiKeychain(
		bearerkeychain.New(os.Getenv("GCRCLEANER_TOKEN")),
		gcrauthn.DefaultKeychain,
		gcrgoogle.Keychain,
	)

	cleaner, err := gcrcleaner.NewCleaner(keychain, logger, concurrency)
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
