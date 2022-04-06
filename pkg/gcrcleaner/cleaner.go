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
	"context"
	"errors"
	"fmt"
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
	logger      *Logger
	concurrency int
}

// NewCleaner creates a new GCR cleaner with the given token provider and
// concurrency.
func NewCleaner(auther gcrauthn.Authenticator, logger *Logger, c int) (*Cleaner, error) {
	return &Cleaner{
		auther:      auther,
		concurrency: c,
		logger:      logger,
	}, nil
}

// Clean deletes old images from GCR that are (un)tagged and older than "since"
// and higher than the "keep" amount.
func (c *Cleaner) Clean(repo string, since time.Time, keep int, tagFilter TagFilter, dryRun bool) ([]string, error) {
	gcrrepo, err := gcrname.NewRepository(repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get repo %s: %w", repo, err)
	}
	c.logger.Debug("computed repo", "repo", gcrrepo.Name())

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

	var manifests = make([]*manifest, 0, len(tags.Manifests))
	for k, m := range tags.Manifests {
		manifests = append(manifests, &manifest{repo, k, m})
	}

	// Sort manifest by Created from the most recent to the least
	sort.Slice(manifests, func(i, j int) bool {
		return manifests[j].Info.Created.Before(manifests[i].Info.Created)
	})

	for _, m := range manifests {
		// Store copy of manifest for thread safety in delete job pool
		m := m

		c.logger.Debug("processing manifest",
			"repo", repo,
			"digest", m.Digest,
			"tags", m.Info.Tags,
			"uploaded", m.Info.Uploaded.Format(time.RFC3339))

		if c.shouldDelete(m, since, tagFilter) {
			// Keep a certain amount of images
			if keepCount < keep {
				c.logger.Debug("skipping deletion because of keep count",
					"repo", repo,
					"digest", m.Digest,
					"keep", keep,
					"keep_count", keepCount)

				keepCount++
				continue
			}

			// Deletes all tags before deleting the image
			for _, tag := range m.Info.Tags {
				c.logger.Debug("deleting tag",
					"repo", repo,
					"digest", m.Digest,
					"tag", tag)

				tagged := gcrrepo.Tag(tag)
				if !dryRun {
					if err := c.deleteOne(tagged); err != nil {
						return nil, fmt.Errorf("failed to delete %s: %w", tagged, err)
					}
				}

				deletedLock.Lock()
				deleted = append(deleted, tagged.Identifier())
				deletedLock.Unlock()
			}

			digest := m.Digest
			ref := gcrrepo.Digest(digest)
			pool.Submit(func() {
				// Do not process if previous invocations failed. This prevents a large
				// build-up of failed requests and rate limit exceeding (e.g. bad auth).
				errsLock.RLock()
				if len(errs) > 0 {
					errsLock.RUnlock()
					return
				}
				errsLock.RUnlock()

				c.logger.Debug("deleting digest",
					"repo", repo,
					"digest", m.Digest)

				if !dryRun {
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
				}

				deletedLock.Lock()
				deleted = append(deleted, digest)
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

	sort.Strings(deleted)

	return deleted, nil
}

type manifest struct {
	Repo   string
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

// shouldDelete returns true if the manifest was created before the given
// timestamp and either has no tags or has tags that match the given filter.
func (c *Cleaner) shouldDelete(m *manifest, since time.Time, tagFilter TagFilter) bool {
	// Immediately exclude images that have been uploaded after the given time.
	if uploaded := m.Info.Uploaded.UTC(); uploaded.After(since) {
		c.logger.Debug("should not delete",
			"repo", m.Repo,
			"digest", m.Digest,
			"reason", "too new",
			"since", since.Format(time.RFC3339),
			"uploaded", uploaded.Format(time.RFC3339),
			"delta", uploaded.Sub(since).String())
		return false
	}

	// If there are no tags, it should be deleted.
	if len(m.Info.Tags) == 0 {
		c.logger.Debug("should delete",
			"repo", m.Repo,
			"digest", m.Digest,
			"reason", "no tags")
		return true
	}

	// If tagged images are allowed and the given filter matches the list of tags,
	// this is a deletion candidate. The default tag filter is to reject all
	// strings.
	if tagFilter.Matches(m.Info.Tags) {
		c.logger.Debug("should delete",
			"repo", m.Repo,
			"digest", m.Digest,
			"reason", "matches tag filter",
			"tag_filter", tagFilter.Name())
		return true
	}

	// If we got this far, it'ts not a viable deletion candidate.
	c.logger.Debug("should not delete",
		"repo", m.Repo,
		"digest", m.Digest,
		"reason", "no filter matches")
	return false
}

func (c *Cleaner) ListChildRepositories(ctx context.Context, rootRepository string) ([]string, error) {
	rootRepo, err := gcrname.NewRepository(rootRepository)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository %s: %w", rootRepository, err)
	}

	registry, err := gcrname.NewRegistry(rootRepo.RegistryStr())
	if err != nil {
		return nil, fmt.Errorf("failed to create registry %s: %w", rootRepo.RegistryStr(), err)
	}

	allRepos, err := gcrremote.Catalog(ctx, registry, gcrremote.WithAuth(c.auther))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch all repositories from registry %s: %w", registry.Name(), err)
	}

	var childRepos = make([]string, 0, len(allRepos))
	for _, repo := range allRepos {
		if strings.HasPrefix(repo, rootRepo.RepositoryStr()) {
			childRepos = append(childRepos, fmt.Sprintf("%s/%s", registry.Name(), repo))
		}
	}

	sort.Strings(childRepos)
	return childRepos, nil
}
