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

package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/sethvargo/gcr-cleaner/pkg/gcrcleaner"
)

func main() {
	// Disable timestamps in go logs because stackdriver has them already.
	log.SetFlags(log.Flags() &^ (log.Ldate | log.Ltime))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := "0.0.0.0:" + port

	tokenProvider := gcrcleaner.TokenProviderMetadataServer()
	concurrency := runtime.NumCPU()
	cleaner, err := gcrcleaner.NewCleaner(tokenProvider, concurrency)
	if err != nil {
		log.Fatalf("failed to create cleaner: %s", err)
	}

	cleanerServer, err := gcrcleaner.NewServer(cleaner)
	if err != nil {
		log.Fatalf("failed to create server: %s", err)
	}

	cache := newTimerCache(30 * time.Minute)

	mux := http.NewServeMux()
	mux.Handle("/http", cleanerServer.HTTPHandler())
	mux.Handle("/pubsub", cleanerServer.PubSubHandler(cache))

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		log.Printf("server is listening on %s\n", port)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
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
