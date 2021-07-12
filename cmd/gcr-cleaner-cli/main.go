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

// Package main defines the CLI interface for GCR Cleaner.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"time"

	gcrauthn "github.com/google/go-containerregistry/pkg/authn"
	gcrgoogle "github.com/google/go-containerregistry/pkg/v1/google"
	"github.com/hashicorp/go-multierror"
	"github.com/sethvargo/gcr-cleaner/pkg/gcrcleaner"
)

var (
	stdout = os.Stdout
	stderr = os.Stderr

	tokenPtr       = flag.String("token", os.Getenv("GCRCLEANER_TOKEN"), "Authentication token")
	repoPtr        = flag.String("repo", "", "Repository name")
	recursivePtr   = flag.Bool("recursive", false, "Clean all sub-repositories under the -repo root")
	gracePtr       = flag.Duration("grace", 0, "Grace period")
	allowTaggedPtr = flag.Bool("allow-tagged", false, "Delete tagged images")
	keepPtr        = flag.Int("keep", 0, "Minimum to keep")
	tagFilterPtr   = flag.String("tag-filter", "", "Tags pattern to clean")
)

func main() {
	flag.Parse()

	if err := realMain(); err != nil {
		fmt.Fprintf(stderr, "%s\n", err)
		os.Exit(1)
	}
}

func realMain() error {
	if *repoPtr == "" {
		return fmt.Errorf("missing -repo")
	}

	if *allowTaggedPtr == false && *tagFilterPtr != "" {
		return fmt.Errorf("-allow-tagged must be true when -tag-filter is declared")
	}

	tagFilterRegexp, err := regexp.Compile(*tagFilterPtr)
	if err != nil {
		return fmt.Errorf("failed to parse -tag-filter: %w", err)
	}

	// Try to find the "best" authentication.
	var auther gcrauthn.Authenticator
	if *tokenPtr != "" {
		auther = &gcrauthn.Bearer{Token: *tokenPtr}
	} else {
		var err error
		auther, err = gcrgoogle.NewEnvAuthenticator()
		if err != nil {
			return fmt.Errorf("failed to setup auther: %w", err)
		}
	}

	concurrency := runtime.NumCPU()
	cleaner, err := gcrcleaner.NewCleaner(auther, concurrency)
	if err != nil {
		return fmt.Errorf("failed to create cleaner: %w", err)
	}

	// Convert duration to a negative value, since we're about to "add" it to the
	// since time.
	sub := time.Duration(*gracePtr)
	if *gracePtr > 0 {
		sub = sub * -1
	}
	since := time.Now().UTC().Add(sub)

	// Gather the repositories
	var repositories = make([]string, 0)
	repositories = append(repositories, *repoPtr)
	if *recursivePtr {
		childRepos, err := cleaner.ListChildRepositories(context.Background(), *repoPtr)
		if err != nil {
			return err
		}
		repositories = append(repositories, childRepos...)
	}

	// Do the deletion.
	var result *multierror.Error
	for _, repo := range repositories {
		fmt.Fprintf(stdout, "%s: deleting refs since %s\n", repo, since)
		deleted, err := cleaner.Clean(repo, since, *allowTaggedPtr, *keepPtr, tagFilterRegexp)
		if err != nil {
			result = multierror.Append(result, err)
		}
		fmt.Fprintf(stdout, "%s: successfully deleted %d refs\n", repo, len(deleted))
	}

	return result.ErrorOrNil()
}
