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
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/gcr-cleaner/internal/version"
	"github.com/GoogleCloudPlatform/gcr-cleaner/internal/worker"
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
// [2]: https://buildpacks.io/docs/features/reproducibility/
var dockerExistence = time.Date(2013, time.March, 20, 0, 0, 0, 0, time.UTC)

// userAgent is the HTTP user agent.
var userAgent = fmt.Sprintf("%s/%s (+https://github.com/GoogleCloudPlatform/gcr-cleaner)",
	version.Name, version.Version)

// Cleaner is a gcr cleaner.
type Cleaner struct {
	keychain    gcrauthn.Keychain
	logger      *Logger
	concurrency int64
}

// NewCleaner creates a new GCR cleaner with the given token provider and
// concurrency.
func NewCleaner(keychain gcrauthn.Keychain, logger *Logger, concurrency int64) (*Cleaner, error) {
	return &Cleaner{
		keychain:    keychain,
		concurrency: concurrency,
		logger:      logger,
	}, nil
}

// Clean deletes old images from GCR that are (un)tagged and older than "since"
// and higher than the "keep" amount.
func (c *Cleaner) Clean(ctx context.Context, repo string, since time.Time, keep int64, tagFilter TagFilter, dryRun bool) ([]string, error) {
	gcrrepo, err := gcrname.NewRepository(repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get repo %s: %w", repo, err)
	}
	c.logger.Debug("computed repo", "repo", gcrrepo.Name())

	tags, err := gcrgoogle.List(gcrrepo,
		gcrgoogle.WithContext(ctx),
		gcrgoogle.WithUserAgent(userAgent),
		gcrgoogle.WithAuthFromKeychain(c.keychain))
	if err != nil {
		return nil, fmt.Errorf("failed to list tags for repo %s: %w", repo, err)
	}

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
	manifestListForLog := make([]map[string]any, 0, len(manifests))
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

	// Create the worker.
	w := worker.New[string](c.concurrency)

	var keepCount = int64(0)
	var digestsToDelete []string
	var toRetry []string
	var toRetryLock sync.Mutex

	// Delete all the manifests.
	for _, m := range manifests {
		m := m

		c.logger.Debug("processing manifest",
			"repo", repo,
			"digest", m.Digest,
			"tags", m.Info.Tags,
			"created", m.Info.Created.Format(time.RFC3339),
			"uploaded", m.Info.Uploaded.Format(time.RFC3339))

		// Do nothing if this is not a candidate.
		if !c.shouldDelete(m, since, tagFilter) {
			c.logger.Debug("skipping deletion because of filters",
				"repo", repo,
				"digest", m.Digest,
				"tags", m.Info.Tags)
			continue
		}

		// Keep a certain amount of images.
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

		// Make note that we need to delete this digest.
		digestsToDelete = append(digestsToDelete, m.Digest)

		// Delete all tags before attempting to delete the digests later.
		for _, tag := range m.Info.Tags {
			tag := tag

			if err := w.Do(ctx, func() (string, error) {
				c.logger.Debug("deleting tag",
					"repo", repo,
					"digest", m.Digest,
					"tag", tag)

				tagged := gcrrepo.Tag(tag)
				if !dryRun {
					if err := c.deleteOne(ctx, tagged); err != nil {
						return "", fmt.Errorf("failed to delete tag %s: %w", tagged, err)
					}
				}
				return tagged.Identifier(), nil
			}); err != nil {
				return nil, err
			}
		}
	}

	// Delete the digest. This is only safe after all the tags have been
	// deleted, so wait for that to finish first.
	if err := w.Wait(ctx); err != nil {
		return nil, err
	}
	for _, digest := range digestsToDelete {
		digest := digest

		if err := w.Do(ctx, func() (string, error) {
			c.logger.Debug("deleting digest",
				"repo", repo,
				"digest", digest)

			grcdigest := gcrrepo.Digest(digest)
			if !dryRun {
				if err := c.deleteOne(ctx, grcdigest); err != nil {
					// We cannot delete fat manifests which still have images. There's no
					// easy way to build a DAG of these, so just push them onto the end
					// and retry again later.
					if strings.Contains(err.Error(), "GOOGLE_MANIFEST_DANGLING_PARENT_IMAGE") {
						c.logger.Debug("failed to delete digest due to dangling parent, retrying later",
							"repo", repo,
							"digest", digest)

						toRetryLock.Lock()
						toRetry = append(toRetry, digest)
						toRetryLock.Unlock()
						return "", nil
					}

					return "", fmt.Errorf("failed to delete digest %s: %w", digest, err)
				}
			}
			return grcdigest.Identifier(), nil
		}); err != nil {
			return nil, err
		}
	}

	// Wait for all those deletions to finish.
	if err := w.Wait(ctx); err != nil {
		return nil, err
	}

	// Perform any retries.
	for i := 0; i < 3; i++ {
		if len(toRetry) == 0 {
			break
		}

		c.logger.Debug("retrying failed deletions",
			"attempt", i+1,
			"toRetry", toRetry)

		// We don't need as many pre-flight checks, since these entries were already
		// marked for deletion.
		toRetryCopy := make([]string, 0, len(toRetry))
		for _, digest := range toRetry {
			digest := digest

			if err := w.Do(ctx, func() (string, error) {
				c.logger.Debug("deleting digest (retry)",
					"repo", repo,
					"digest", digest)

				grcdigest := gcrrepo.Digest(digest)
				if !dryRun {
					if err := c.deleteOne(ctx, grcdigest); err != nil {
						// We cannot delete fat manifests which still have images. There's no
						// easy way to build a DAG of these, so just push them onto the end
						// and retry again later.
						if strings.Contains(err.Error(), "GOOGLE_MANIFEST_DANGLING_PARENT_IMAGE") {
							toRetryLock.Lock()
							toRetryCopy = append(toRetryCopy, digest)
							toRetryLock.Unlock()
							return "", nil
						}
						return "", fmt.Errorf("failed to delete digest %s: %w", digest, err)
					}
				}
				return grcdigest.Identifier(), nil
			}); err != nil {
				return nil, err
			}
		}

		// Wait for all those deletions to finish.
		if err := w.Wait(ctx); err != nil {
			return nil, err
		}

		// Update to the new retry list.
		toRetry = toRetryCopy
	}

	// Wait for everything to finish.
	results, err := w.Done(ctx)
	if err != nil {
		return nil, err
	}

	// Gather the results.
	deleted := make([]string, 0, len(results))
	errs := make([]error, 0, len(results))
	for _, result := range results {
		if result.Error != nil {
			errs = append(errs, result.Error)
			continue
		}

		if result.Value != "" {
			deleted = append(deleted, result.Value)
		}
	}

	// Aggregate any errors.
	if err := ErrsToError(errs); err != nil {
		return nil, err
	}

	// Return the list of deleted entries.
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
		gcrremote.WithContext(ctx),
		gcrremote.WithUserAgent(userAgent),
		gcrremote.WithAuthFromKeychain(c.keychain),
		gcrremote.WithJobs(int(c.concurrency))); err != nil {
		return err
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

	// Perform lookup in parallel.
	w := worker.New[[]string](c.concurrency)

	// Iterate through each registry, query the entire registry (yea, that's how
	// you "search"), and collect a list of candidate repos.
	for _, registry := range registriesMap {
		registry := registry

		if err := w.Do(ctx, func() ([]string, error) {
			c.logger.Debug("listing child repositories for registry",
				"registry", registry.Name())

			// List all repos in the registry.
			allRepos, err := gcrremote.Catalog(ctx, *registry,
				gcrremote.WithContext(ctx),
				gcrremote.WithUserAgent(userAgent),
				gcrremote.WithAuthFromKeychain(c.keychain),
				gcrremote.WithJobs(int(c.concurrency)))
			if err != nil {
				return nil, fmt.Errorf("failed to list child repositories for registry %s: %w", registry, err)
			}

			c.logger.Debug("found child repositories for registry",
				"registry", registry.Name(),
				"repos", allRepos)

			// Search through each repository and append any repository that matches any
			// of the prefixes defined by roots.
			var candidateRepos []string
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

				if !hasPrefix {
					c.logger.Debug("skipping repository candidate (does not match any roots)",
						"registry", registry.Name(),
						"repo", repo)
					continue
				}

				c.logger.Debug("appending new repository candidate",
					"registry", registry.Name(),
					"repo", repo)
				candidateRepos = append(candidateRepos, fullRepoName)
			}
			return candidateRepos, nil
		}); err != nil {
			return nil, err
		}
	}

	// Wait for everything to finish.
	results, err := w.Done(ctx)
	if err != nil {
		return nil, err
	}

	// Gather the results.
	reposMap := make(map[string]struct{})
	errs := make([]error, 0, len(results))
	for _, result := range results {
		if result.Error != nil {
			errs = append(errs, result.Error)
			continue
		}

		for _, v := range result.Value {
			if v != "" {
				reposMap[v] = struct{}{}
			}
		}
	}

	// Aggregate any errors.
	if err := ErrsToError(errs); err != nil {
		return nil, err
	}

	// De-duplicate and sort the list.
	repos := make([]string, 0, len(reposMap))
	for repo := range reposMap {
		repos = append(repos, repo)
	}
	sort.Strings(repos)
	return repos, nil
}

// ErrsToError converts a list of errors into a single error. If the list is
// empty, it returns nil. If the list contains exactly one error, it returns
// that error. Otherwise it returns a bulleted list of the sorted errors, but
// the original error contexts are discarded.
func ErrsToError(errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		errMessages := make([]string, len(errs))
		for i, err := range errs {
			errMessages[i] = err.Error()
		}
		sort.Strings(errMessages)

		var b strings.Builder
		fmt.Fprintf(&b, "%d errors occurred:\n", len(errs))
		for _, msg := range errMessages {
			fmt.Fprintf(&b, "  * %s\n", msg)
		}
		return fmt.Errorf(b.String())
	}
}
