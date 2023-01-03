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
	"sort"
	"strings"
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
	logLevel = os.Getenv("GCRCLEANER_LOG")
)

var (
	reposMap = make(map[string]struct{}, 4)

	tokenPtr       = flag.String("token", os.Getenv("GCRCLEANER_TOKEN"), "Authentication token")
	recursivePtr   = flag.Bool("recursive", false, "Clean all sub-repositories under the -repo root")
	gracePtr       = flag.Duration("grace", 0, "Grace period")
	tagFilterAny   = flag.String("tag-filter-any", "", "Delete images where any tag matches this regular expression")
	tagFilterAll   = flag.String("tag-filter-all", "", "Delete images where all tags match this regular expression")
	keepPtr        = flag.Int64("keep", 0, "Minimum to keep")
	dryRunPtr      = flag.Bool("dry-run", false, "Do a noop on delete api call")
	concurrencyPtr = flag.Int64("concurrency", 20, "Concurrent requests (defaults to number of CPUs)")
	versionPtr     = flag.Bool("version", false, "Print version information and exit")
)

func main() {
	logger := gcrcleaner.NewLogger(logLevel, stderr, stdout)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	flag.Func("repo", "Repository name", func(s string) error {
		parts := strings.Split(s, ",")
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				reposMap[t] = struct{}{}
			}
		}
		return nil
	})

	flag.Usage = func() {
		w := flag.CommandLine.Output()
		fmt.Fprintf(w, "Usage of %s:\n\n", os.Args[0])
		fmt.Fprintf(w, "  Deletes untagged or stale images from a Docker registry.\n\n")
		fmt.Fprintf(w, "Options:\n\n")

		flag.VisitAll(func(f *flag.Flag) {
			if strings.HasPrefix(f.Usage, "DEPRECATED") {
				return
			}

			fmt.Fprintf(w, "  -%v\n", f.Name)
			fmt.Fprintf(w, "      %s\n\n", f.Usage)
		})
	}

	flag.Parse()

	if *versionPtr {
		fmt.Fprintf(stderr, "%s\n", version.HumanVersion)
		os.Exit(0)
	}

	if err := realMain(ctx, logger); err != nil {
		cancel()

		fmt.Fprintf(stderr, "%s\n", err)
		os.Exit(1)
	}
}

func realMain(ctx context.Context, logger *gcrcleaner.Logger) error {
	logger.Debug("cli is starting", "version", version.HumanVersion)
	defer logger.Debug("cli finished")

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

	tagFilter, err := gcrcleaner.BuildTagFilter(*tagFilterAny, *tagFilterAll)
	if err != nil {
		return fmt.Errorf("failed to parse tag filter: %w", err)
	}

	keychain := gcrauthn.NewMultiKeychain(
		bearerkeychain.New(*tokenPtr),
		gcrauthn.DefaultKeychain,
		gcrgoogle.Keychain,
	)

	cleaner, err := gcrcleaner.NewCleaner(keychain, logger, *concurrencyPtr)
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

	// Gather the repositories.
	if *recursivePtr {
		logger.Debug("gathering child repositories recursively")

		allRepos, err := cleaner.ListChildRepositories(ctx, repos)
		if err != nil {
			return err
		}
		logger.Debug("recursively listed child repositories",
			"in", repos,
			"out", allRepos)

		// This is safe because ListChildRepositories is guaranteed to include at
		// least the list repos given to it.
		repos = allRepos
	}

	// Log dry-run mode.
	if *dryRunPtr {
		fmt.Fprintf(stderr, "WARNING: Running in dry-run mode - nothing will "+
			"actually be cleaned!\n\n")
	}

	fmt.Fprintf(stdout, "Deleting refs older than %s on %d repo(s)...\n\n",
		since.Format(time.RFC3339), len(repos))

	// Do the deletion.
	var errs []error
	for i, repo := range repos {
		fmt.Fprintf(stdout, "%s\n", repo)
		deleted, err := cleaner.Clean(ctx, repo, since, *keepPtr, tagFilter, *dryRunPtr)
		if err != nil {
			errs = append(errs, err)
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

	return gcrcleaner.ErrsToError(errs)
}
