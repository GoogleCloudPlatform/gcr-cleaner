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
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"time"

	gcrauthn "github.com/google/go-containerregistry/pkg/authn"
	gcrgoogle "github.com/google/go-containerregistry/pkg/v1/google"
	"github.com/sethvargo/gcr-cleaner/pkg/gcrcleaner"
)

func main() {
	logger := gcrcleaner.NewLogger(os.Stderr, os.Stdout)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port

	var auther gcrauthn.Authenticator
	if token := os.Getenv("GCRCLEANER_TOKEN"); token != "" {
		auther = &gcrauthn.Bearer{Token: token}
	} else {
		var err error
		auther, err = gcrgoogle.NewEnvAuthenticator()
		if err != nil {
			logger.Fatal("failed to setup auther", "error", err)
		}
	}

	concurrency := runtime.NumCPU()
	cleaner, err := gcrcleaner.NewCleaner(auther, concurrency)
	if err != nil {
		logger.Fatal("failed to create cleaner", "error", err)
	}

	cleanerServer, err := gcrcleaner.NewServer(cleaner)
	if err != nil {
		logger.Fatal("failed to create server", "error", err)
	}

	cache := gcrcleaner.NewTimerCache(5 * time.Minute)

	mux := http.NewServeMux()
	mux.Handle("/http", cleanerServer.HTTPHandler())
	mux.Handle("/pubsub", cleanerServer.PubSubHandler(cache))

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		logger.Debug("server is listening", "port", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server exited", "error", err)
		}
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt)

	<-signalCh

	logger.Info("server received stop, shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Fatal("failed to shutdown server", "error", err)
	}
}
