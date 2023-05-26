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

import "time"

type Decider interface {
	ShouldDelete(*Manifest) (bool, error)
}

type DefaultDecider struct {
	Since            time.Time
	TagFilter        TagFilter
	TagFilterExclude bool
	Logger           *Logger
}

func (d *DefaultDecider) ShouldDelete(m *Manifest) (bool, error) {
	// Immediately exclude images that have been uploaded after the given time.
	if uploaded := m.Info.Uploaded.UTC(); uploaded.After(d.Since) {
		d.Logger.Debug("should not delete",
			"repo", m.Repo,
			"digest", m.Digest,
			"reason", "too new",
			"since", d.Since.Format(time.RFC3339),
			"created", m.Info.Created.Format(time.RFC3339),
			"uploaded", uploaded.Format(time.RFC3339),
			"delta", uploaded.Sub(d.Since).String())
		return false, nil
	}

	// If there are no tags, it should be deleted.
	if len(m.Info.Tags) == 0 {
		d.Logger.Debug("should delete",
			"repo", m.Repo,
			"digest", m.Digest,
			"reason", "no tags")
		return true, nil
	}

	// If tagged images are allowed and the given filter matches the list of tags,
	// this is a deletion candidate. The default tag filter is to reject all
	// strings.
	if d.TagFilter.Matches(m.Info.Tags) && !d.TagFilterExclude {
		d.Logger.Debug("should delete",
			"repo", m.Repo,
			"digest", m.Digest,
			"reason", "matches tag filter",
			"tags", m.Info.Tags,
			"tag_filter", d.TagFilter.Name())
		return true, nil
	}
	if !d.TagFilter.Matches(m.Info.Tags) && d.TagFilterExclude {
		d.Logger.Debug("should delete",
			"repo", m.Repo,
			"digest", m.Digest,
			"reason", "matches tag filter",
			"tags", m.Info.Tags,
			"tag_filter", d.TagFilter.Name())
		return true, nil
	}

	// If we got this far, it'ts not a viable deletion candidate.
	d.Logger.Debug("should not delete",
		"repo", m.Repo,
		"digest", m.Digest,
		"reason", "no filter matches")
	return false, nil
}
