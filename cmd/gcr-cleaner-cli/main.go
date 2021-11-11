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
	"os/signal"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	gcrauthn "github.com/google/go-containerregistry/pkg/authn"
	gcrgoogle "github.com/google/go-containerregistry/pkg/v1/google"
	"github.com/hashicorp/go-multierror"
	"github.com/sethvargo/gcr-cleaner/pkg/gcrcleaner"
)

var (
	stdout = os.Stdout
	stderr = os.Stderr

	reposMap = make(map[string]struct{}, 4)

	tokenPtr       = flag.String("token", os.Getenv("GCRCLEANER_TOKEN"), "Authentication token")
	recursivePtr   = flag.Bool("recursive", false, "Clean all sub-repositories under the -repo root")
	gracePtr       = flag.Duration("grace", 0, "Grace period")
	allowTaggedPtr = flag.Bool("allow-tagged", false, "Delete tagged images")
	keepPtr        = flag.Int("keep", 0, "Minimum to keep")
	tagFilterPtr   = flag.String("tag-filter", "", "Tags pattern to clean")
	dryRunPtr      = flag.Bool("dry-run", false, "Do a noop on delete api call")
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	flag.Func("repo", "Repository name", func(s string) error {
		parts := strings.Split(s, ",")
		for _, p := range parts {
			reposMap[strings.TrimSpace(p)] = struct{}{}
		}
		return nil
	})

	flag.Parse()

	if err := realMain(ctx); err != nil {
		cancel()

		fmt.Fprintf(stderr, "%s\n", err)
		os.Exit(1)
	}
}

func realMain(ctx context.Context) error {
	if args := flag.Args(); len(args) > 0 {
		return fmt.Errorf("expected zero arguments, got %d: %q", len(args), args)
	}

	if len(reposMap) == 0 {
		return fmt.Errorf("missing -repo")
	}

	repos := make([]string, 0, len(reposMap))
	for k := range reposMap {
		repos = append(repos, k)
	}
	sort.Strings(repos)

	if !*allowTaggedPtr && *tagFilterPtr != "" {
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
	if *recursivePtr {
		for _, repo := range repos {
			childRepos, err := cleaner.ListChildRepositories(ctx, repo)
			if err != nil {
				return err
			}
			repos = append(repos, childRepos...)
		}
	}

	// Log dry-run mode.
	if *dryRunPtr {
		fmt.Fprintf(stderr, "WARNING: Running in dry-run mode - nothing will "+
			"actually be cleaned!\n\n")
	}

	fmt.Fprintf(stdout, "Deleting refs since %s on %d repo(s)...\n\n",
		since.Format(time.RFC3339), len(repos))

	// Do the deletion.
	var result *multierror.Error
	for i, repo := range repos {
		fmt.Fprintf(stdout, "%s\n", repo)
		deleted, err := cleaner.Clean(repo, since, *allowTaggedPtr, *keepPtr, tagFilterRegexp, *dryRunPtr)
		if err != nil {
			result = multierror.Append(result, err)
		}

		if len(deleted) > 0 {
			for _, val := range deleted {
				fmt.Fprintf(stdout, "  ✓ %s\n", val)
			}
		} else {
			fmt.Fprintf(stdout, "  ✗ no refs were deleted\n")
		}

		if i != len(repos)-1 {
			fmt.Fprintf(stdout, "\n")
		}
	}
	return result.ErrorOrNil()
}
