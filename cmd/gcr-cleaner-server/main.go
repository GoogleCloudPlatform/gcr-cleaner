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
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"time"

	gcrauthn "github.com/google/go-containerregistry/pkg/authn"
	gcrgoogle "github.com/google/go-containerregistry/pkg/v1/google"
	"github.com/sethvargo/gcr-cleaner/pkg/gcrcleaner"
)

func main() {
	// Disable timestamps in go logs because stackdriver has them already.
	log.SetFlags(log.Flags() &^ (log.Ldate | log.Ltime))

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
			log.Fatalf("failed to setup auther: %s", err)
		}
	}

	concurrency, err := strconv.Atoi(os.Getenv("CONCURRENCY"))
	if err != nil {
		log.Println("WARNING: CONCURRENCY must be a valid integer")
	}
	if concurrency == 0 {
		concurrency = runtime.NumCPU()
	}
	cleaner, err := gcrcleaner.NewCleaner(auther, concurrency)
	if err != nil {
		log.Fatalf("failed to create cleaner: %s", err)
	}

	cleanerServer, err := gcrcleaner.NewServer(cleaner)
	if err != nil {
		log.Fatalf("failed to create server: %s", err)
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
		log.Printf("server is listening on %s\n", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server exited: %s", err)
		}
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt)

	<-signalCh

	log.Printf("received stop, shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("failed to shutdown server: %s", err)
	}
}
