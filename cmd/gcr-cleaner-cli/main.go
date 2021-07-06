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
	"flag"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"

	gcrauthn "github.com/google/go-containerregistry/pkg/authn"
	gcrgoogle "github.com/google/go-containerregistry/pkg/v1/google"
	"github.com/sethvargo/gcr-cleaner/pkg/gcrcleaner"
)

var (
	stdout = os.Stdout
	stderr = os.Stderr

	tokenPtr             = flag.String("token", os.Getenv("GCRCLEANER_TOKEN"), "Authentication token")
	repoPtr              = flag.String("repo", "", "Repository name")
	gracePtr             = flag.Duration("grace", 0, "Grace period")
	allowTaggedPtr       = flag.Bool("allow-tagged", false, "Delete tagged images")
	keepPtr              = flag.Int("keep", 0, "Minimum to keep")
	tagFilterPtr         = flag.String("tag-filter", "", "Tags pattern to clean")
	tagFilterMatchAnyPtr = flag.Bool("tag-filter-match-any", false, "Delete image if any one tag matches the tags pattern")
	excludedTagsPtr      = flag.String("excluded-tags", "", "Tags to be excluded")
	dryRunPtr            = flag.Bool("dry-run", false, "Dry Run")
	concurrencyPtr       = flag.Int("concurrency", 0, "Number of parallel deletions")
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

	if *allowTaggedPtr == false && (*tagFilterPtr != "" || *excludedTagsPtr != "") {
		return fmt.Errorf("-allow-tagged must be true when -tag-filter and/or -exclude-tags are declared")
	}

	tagFilterRegexp, err := regexp.Compile(*tagFilterPtr)
	if err != nil {
		return fmt.Errorf("failed to parse -tag-filter: %w", err)
	}

	excludedTags := map[string]struct{}{}
	if *excludedTagsPtr != "" {
		for _, v := range strings.Split(*excludedTagsPtr, ",") {
			excludedTags[v] = struct{}{}
		}
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

	concurrency := *concurrencyPtr
	if concurrency == 0 {
		concurrency = runtime.NumCPU()
	}
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

	// Do the deletion.
	if *dryRunPtr {
		fmt.Fprintf(stdout, "%s: (DRY RUN) deleting refs since %s\n", *repoPtr, since)
	} else {
		fmt.Fprintf(stdout, "%s: deleting refs since %s\n", *repoPtr, since)
	}
	deleted, err := cleaner.Clean(*repoPtr, since, *allowTaggedPtr, *keepPtr, tagFilterRegexp, *tagFilterMatchAnyPtr, excludedTags, *dryRunPtr)
	if err != nil {
		return err
	}

	if *dryRunPtr {
		fmt.Fprintf(stdout, "%s: (DRY RUN) would delete %d refs\n", *repoPtr, len(deleted))
		for _, v := range deleted {
			fmt.Fprintf(stdout, "Digest:\n    %s\n", v.Digest)
			if len(v.Info.Tags) > 0 {
				fmt.Fprintln(stdout, "With tags:")
				for _, t := range v.Info.Tags {
					fmt.Fprintf(stdout, "    %s\n", t)
				}
			}
			fmt.Fprintf(stdout, "---\n")
		}
	} else {
		fmt.Fprintf(stdout, "%s: successfully deleted %d refs", *repoPtr, len(deleted))
	}

	return nil
}
