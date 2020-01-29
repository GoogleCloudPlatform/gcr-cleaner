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

package gcrcleaner

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gammazero/workerpool"
	gcrauthn "github.com/google/go-containerregistry/pkg/authn"
	gcrname "github.com/google/go-containerregistry/pkg/name"
	gcrgoogle "github.com/google/go-containerregistry/pkg/v1/google"
	gcrremote "github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/pkg/errors"
)

// Cleaner is a gcr cleaner.
type Cleaner struct {
	auther      gcrauthn.Authenticator
	concurrency int
}

// NewCleaner creates a new GCR cleaner with the given token provider and
// concurrency.
func NewCleaner(p TokenProvider, c int) (*Cleaner, error) {
	return &Cleaner{
		auther:      bearerAuthenticator(p),
		concurrency: c,
	}, nil
}

// Clean deletes old images from GCR that are untagged and older than "since".
func (c *Cleaner) Clean(repo string, since time.Time, allow_tagged bool) ([]string, error) {
	gcrrepo, err := gcrname.NewRepository(repo)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get repo %s", repo)
	}

	tags, err := gcrgoogle.List(gcrrepo, gcrgoogle.WithAuth(c.auther))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list tags for repo %s", repo)
	}

	// Create a worker pool for parallel deletion
	pool := workerpool.New(c.concurrency)

	var deleted = make([]string, 0, len(tags.Manifests))
	var deletedLock sync.Mutex
	var errs = make(map[string]error)
	var errsLock sync.RWMutex

	for k, m := range tags.Manifests {
		if c.shouldDelete(m, since, allow_tagged) {
			ref := repo + "@" + k
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
					cause := errors.Cause(err).Error()

					errsLock.Lock()
					if _, ok := errs[cause]; !ok {
						errs[cause] = err
						errsLock.Unlock()
						return
					}
					errsLock.Unlock()
				}

				deletedLock.Lock()
				deleted = append(deleted, k)
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
			return nil, errors.New(errStrings[0])
		}

		return nil, errors.Errorf("%d errors occurred: %s",
			len(errStrings), strings.Join(errStrings, ", "))
	}

	return deleted, nil
}

// deleteOne deletes a single repo ref using the supplied auth.
func (c *Cleaner) deleteOne(ref string) error {
	name, err := gcrname.ParseReference(ref)
	if err != nil {
		return errors.Wrapf(err, "failed to parse reference %s", ref)
	}

	if err := gcrremote.Delete(name, c.auther, http.DefaultTransport); err != nil {
		return errors.Wrapf(err, "failed to delete %s", name)
	}

	return nil
}

// shouldDelete returns true if the manifest has no tags and is before the
// requested time.
func (c *Cleaner) shouldDelete(m gcrgoogle.ManifestInfo, since time.Time, allow_tag bool) bool {
	return (allow_tag || len(m.Tags) == 0) && m.Uploaded.UTC().Before(since)
}
