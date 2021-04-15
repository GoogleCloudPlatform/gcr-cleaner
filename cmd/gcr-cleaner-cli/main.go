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
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/pkg/errors"

	gcrauthn "github.com/google/go-containerregistry/pkg/authn"
	"github.com/sethvargo/gcr-cleaner/pkg/gcrcleaner"

	gcrname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/google"
	gcrgoogle "github.com/google/go-containerregistry/pkg/v1/google"

	"os"
)

var (
	stdout = os.Stdout
	stderr = os.Stderr

	tokenPtr       = flag.String("token", os.Getenv("GCRCLEANER_TOKEN"), "Authentication token")
	repoPtr        = flag.String("repo", "", "Repository name")
	registryPtr    = flag.String("registry", "", "Registry name")
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
	if *repoPtr == "" && *registryPtr == "" {
		return fmt.Errorf("missing -repo or -registry")
	}

	if *repoPtr != "" && *registryPtr != "" {
		return fmt.Errorf("only use one of -repo or -registry flags")
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

	var multierror []string
	// walk through all repos in a registry
	if *registryPtr != "" {
		walkFn := func(repo gcrname.Repository, tags *google.Tags, err error) error {
			repoName := repo.String()
			// Do the deletion.
			err = delete(&repoName, since, err, cleaner, tagFilterRegexp)

			// if we have an error with one repo let's continue to gc others
			if err != nil {
				multierror = append(multierror, errors.Wrapf(err, "failed to delete repo %s", *registryPtr).Error())
			}
			return nil
		}
		srcRepo, err := gcrname.NewRepository(*registryPtr)
		if err != nil {
			return errors.Wrapf(err, "failed to create repo %s", *registryPtr)
		}
		if err := google.Walk(srcRepo, walkFn, google.WithAuth(auther)); err != nil {
			return errors.Wrapf(err, "failed to walk repo %s", *registryPtr)
		}
		if len(multierror) > 0 {
			return fmt.Errorf(strings.Join(multierror, "\n"))
		}
	} else {
		// Do the deletion.
		return delete(repoPtr, since, err, cleaner, tagFilterRegexp)
	}

	return nil

}

func delete(repo *string, since time.Time, err error, cleaner *gcrcleaner.Cleaner, tagFilterRegexp *regexp.Regexp) error {
	fmt.Fprintf(stdout, "%s: deleting refs since %s in repo %s\n", *repoPtr, since, *repo)
	deleted, err := cleaner.Clean(*repo, since, *allowTaggedPtr, *keepPtr, tagFilterRegexp)
	if err != nil {
		return errors.Wrapf(err, "failed to clean repo %s", *repo)
	}
	fmt.Fprintf(stdout, "%s: successfully deleted %d refs from repo %s\n", *repoPtr, len(deleted), *repo)
	return nil
}
