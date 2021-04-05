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

// Package gcrcleaner cleans up stale images from a container registry.
package gcrcleaner

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gammazero/workerpool"
	gcrauthn "github.com/google/go-containerregistry/pkg/authn"
	gcrname "github.com/google/go-containerregistry/pkg/name"
	gcrgoogle "github.com/google/go-containerregistry/pkg/v1/google"
	gcrremote "github.com/google/go-containerregistry/pkg/v1/remote"
)

// Cleaner is a gcr cleaner.
type Cleaner struct {
	auther      gcrauthn.Authenticator
	concurrency int
}

// NewCleaner creates a new GCR cleaner with the given token provider and
// concurrency.
func NewCleaner(auther gcrauthn.Authenticator, c int) (*Cleaner, error) {
	return &Cleaner{
		auther:      auther,
		concurrency: c,
	}, nil
}

// Clean deletes old images from GCR that are (un)tagged and older than "since" and
// higher than the "keep" amount.
func (c *Cleaner) Clean(repo string, since time.Time, allowTagged bool, keep int, tagFilterRegexp *regexp.Regexp) ([]string, error) {
	gcrrepo, err := gcrname.NewRepository(repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get repo %s: %w", repo, err)
	}

	tags, err := gcrgoogle.List(gcrrepo, gcrgoogle.WithAuth(c.auther))
	if err != nil {
		return nil, fmt.Errorf("failed to list tags for repo %s: %w", repo, err)
	}

	// Create a worker pool for parallel deletion
	pool := workerpool.New(c.concurrency)

	var keepCount = 0
	var deleted = make([]string, 0, len(tags.Manifests))
	var deletedLock sync.Mutex
	var errs = make(map[string]error)
	var errsLock sync.RWMutex

	var manifests = make([]manifest, 0, len(tags.Manifests))
	for k, m := range tags.Manifests {
		manifests = append(manifests, manifest{k, m})
	}

	// Sort manifest by Created from the most recent to the least
	sort.Slice(manifests, func(i, j int) bool {
		return manifests[j].Info.Created.Before(manifests[i].Info.Created)
	})

	for _, m := range manifests {
		if c.shouldDelete(m.Info, since, allowTagged, tagFilterRegexp) {
			// Keep a certain amount of images
			if keepCount < keep {
				keepCount++
				continue
			}

			// Deletes all tags before deleting the image
			for _, tag := range m.Info.Tags {
				tagged := gcrrepo.Tag(tag)
				if err := c.deleteOne(tagged); err != nil {
					return nil, fmt.Errorf("failed to delete %s: %w", tagged, err)
				}
			}

			ref := gcrrepo.Digest(m.Digest)
			pool.Submit(func() {
				// Do not process if previous invocations failed. This prevents a large
				// build-up of failed requests and rate limit exceeding (e.g. bad auth).
				errsLock.RLock()
				if len(errs) > 0 {
					errsLock.RUnlock()
					return
				}
				errsLock.RUnlock()

				if err := c.deleteOne(ref); err != nil {
					cause := errors.Unwrap(err).Error()

					errsLock.Lock()
					if _, ok := errs[cause]; !ok {
						errs[cause] = err
						errsLock.Unlock()
						return
					}
					errsLock.Unlock()
				}

				deletedLock.Lock()
				deleted = append(deleted, m.Digest)
				deletedLock.Unlock()
			})
		}
	}

	// Wait for everything to finish
	pool.StopWait()

	// Aggregate any errors
	if len(errs) > 0 {
		var errStrings []string
		for _, v := range errs {
			errStrings = append(errStrings, v.Error())
		}

		if len(errStrings) == 1 {
			return nil, fmt.Errorf(errStrings[0])
		}

		return nil, fmt.Errorf("%d errors occurred: %s",
			len(errStrings), strings.Join(errStrings, ", "))
	}

	return deleted, nil
}

type manifest struct {
	Digest string
	Info   gcrgoogle.ManifestInfo
}

// deleteOne deletes a single repo ref using the supplied auth.
func (c *Cleaner) deleteOne(ref gcrname.Reference) error {
	if err := gcrremote.Delete(ref, gcrremote.WithAuth(c.auther)); err != nil {
		return fmt.Errorf("failed to delete %s: %w", ref, err)
	}

	return nil
}

// shouldDelete returns true if the manifest has no tags or allows deletion of tagged images
// and is before the requested time.
func (c *Cleaner) shouldDelete(m gcrgoogle.ManifestInfo, since time.Time, allowTag bool, tagFilterRegexp *regexp.Regexp) bool {
	return (len(m.Tags) == 0 || (allowTag && tagFilterRegexp.MatchString(m.Tags[0]))) && m.Uploaded.UTC().Before(since)
}
