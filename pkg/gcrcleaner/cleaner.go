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

// dockerExistence is date of the first release of Docker[1] (then dotCloud) and
// marks the first possible date in which Docker containers could feasibly have
// been created. We need this because some tools set the container's CreatedDate
// to a very old value[2] and thus sorting by creation date fails.
//
// [1]: https://en.wikipedia.org/wiki/Docker_(software)
//
// [2]: https://buildpacks.io/docs/features/reproducibility/
var dockerExistence = time.Date(2013, time.March, 20, 0, 0, 0, 0, time.UTC)

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
func (c *Cleaner) Clean(ctx context.Context, repo string, since time.Time, keep int, tagFilter TagFilter, dryRun bool) ([]string, error) {
	gcrrepo, err := gcrname.NewRepository(repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get repo %s: %w", repo, err)
	}
	c.logger.Debug("computed repo", "repo", gcrrepo.Name())

	tags, err := gcrgoogle.List(gcrrepo,
		gcrgoogle.WithContext(ctx),
		gcrgoogle.WithAuth(c.auther))
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

	// Sort manifests. If either of the containers were created before Docker even
	// existed, we fall back to the upload date. This can happen with some
	// community build tools. If two containers were created at the same time, we
	// fall back to the upload date. Otherwise, we sort by the container creation
	// date.
	sort.Slice(manifests, func(i, j int) bool {
		jCreated, jUploaded := manifests[j].Info.Created, manifests[j].Info.Uploaded
		iCreated, iUploaded := manifests[i].Info.Created, manifests[i].Info.Uploaded

		// If either container has a CreateTime that predates Docker's existence, or
		// the contains have the same creation time, fallback to the uploaded time.
		if jCreated.Before(dockerExistence) || iCreated.Before(dockerExistence) || jCreated.Equal(iCreated) {
			return jUploaded.Before(iUploaded)
		}

		return jCreated.Before(iCreated)
	})

	// Generate an ordered map
	manifestListForLog := make([]map[string]any, len(manifests))
	for _, m := range manifests {
		manifestListForLog = append(manifestListForLog, map[string]any{
			"repo":     m.Repo,
			"digest":   m.Digest,
			"tags":     m.Info.Tags,
			"created":  m.Info.Created.Format(time.RFC3339),
			"uploaded": m.Info.Uploaded.Format(time.RFC3339),
		})
	}
	c.logger.Debug("computed all manifests",
		"keep", keep,
		"manifests", manifestListForLog)

	for _, m := range manifests {
		// Store copy of manifest for thread safety in delete job pool
		m := m

		c.logger.Debug("processing manifest",
			"repo", repo,
			"digest", m.Digest,
			"tags", m.Info.Tags,
			"created", m.Info.Created.Format(time.RFC3339),
			"uploaded", m.Info.Uploaded.Format(time.RFC3339))

		if c.shouldDelete(m, since, tagFilter) {
			// Keep a certain amount of images
			if keepCount < keep {
				c.logger.Debug("skipping deletion because of keep count",
					"repo", repo,
					"digest", m.Digest,
					"keep", keep,
					"keep_count", keepCount,
					"created", m.Info.Created.Format(time.RFC3339),
					"uploaded", m.Info.Uploaded.Format(time.RFC3339))

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
					if err := c.deleteOne(ctx, tagged); err != nil {
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
					if err := c.deleteOne(ctx, ref); err != nil {
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
func (c *Cleaner) deleteOne(ctx context.Context, ref gcrname.Reference) error {
	if err := gcrremote.Delete(ref,
		gcrremote.WithAuth(c.auther),
		gcrremote.WithContext(ctx)); err != nil {
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
			"created", m.Info.Created.Format(time.RFC3339),
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
			"tags", m.Info.Tags,
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

// ListChildRepositories lists all child repositores for the given roots. Roots
// can be entire registries (e.g. us-docker.pkg.dev) or a subpath within a
// registry (e.g. gcr.io/my-project/my-container).
func (c *Cleaner) ListChildRepositories(ctx context.Context, roots []string) ([]string, error) {
	c.logger.Debug("finding all child repositories", "roots", roots)

	// registriesMap is a cache of registries to all the repos in that registry.
	// Since multiple repos might use the same registry, the result is cached to
	// limit upstream API calls.
	registriesMap := make(map[string]*gcrname.Registry, len(roots))

	// Iterate over each root and attempt to extract the registry component. Some
	// roots will be registries themselves whereas other roots could be a subpath
	// in a registry and we need to extract just the registry part.
	for _, root := range roots {
		registryName := ""

		parts := strings.Split(root, "/")
		switch len(parts) {
		case 0:
			panic("got 0 parts from string split (impossible)")
		case 1:
			// Most likely this is a registry, since it contains no slashes.
			registryName = parts[0]
		default:
			repo, err := gcrname.NewRepository(root, gcrname.StrictValidation)
			if err != nil {
				return nil, fmt.Errorf("failed to parse root repository %q: %w", root, err)
			}

			registryName = repo.RegistryStr()
		}

		registry, err := gcrname.NewRegistry(registryName)
		if err != nil {
			return nil, fmt.Errorf("failed to parse registry name %q: %w", registryName, err)
		}
		registriesMap[registryName] = &registry
	}

	// candidateRepos is the list of full repository names that match any of the
	// given root repositories. This list is appended to so the range function
	// below is psuedo-recursive.
	candidateRepos := make([]string, 0, len(roots))

	// Iterate through each registry, query the entire registry (yea, that's how
	// you "search"), and collect a list of candidate repos.
	for _, registry := range registriesMap {
		c.logger.Debug("listing child repositories for registry",
			"registry", registry.Name())

		// List all repos in the registry.
		allRepos, err := gcrremote.Catalog(ctx, *registry,
			gcrremote.WithAuth(c.auther),
			gcrremote.WithContext(ctx))
		if err != nil {
			return nil, fmt.Errorf("failed to fetch catalog for registry %q: %w", registry.Name(), err)
		}

		c.logger.Debug("found child repositories for registry",
			"registry", registry.Name(),
			"repos", allRepos)

		// Search through each repository and append any repository that matches any
		// of the prefixes defined by roots.
		for _, repo := range allRepos {
			// Compute the full repo name by appending the repo to the registry
			// identifier.
			fullRepoName := registry.Name() + "/" + repo

			hasPrefix := false
			for _, root := range roots {
				if strings.HasPrefix(fullRepoName, root) {
					hasPrefix = true
					break
				}
			}
			if hasPrefix {
				c.logger.Debug("appending new repository candidate",
					"registry", registry.Name(),
					"repo", repo)
				candidateRepos = append(candidateRepos, fullRepoName)
			} else {
				c.logger.Debug("skipping repository candidate (does not match any roots)",
					"registry", registry.Name(),
					"repo", repo)
			}
		}
	}

	// De-duplicate and sort the list.
	sort.Strings(candidateRepos)
	return candidateRepos, nil
}
