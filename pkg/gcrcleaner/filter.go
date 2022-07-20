// Copyright 2021 The GCR Cleaner Authors
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
	"fmt"
	"regexp"
)

// TagFilter is an interface which defines whether a given set of tags matches
// the filter.
type TagFilter interface {
	Name() string
	Matches(tags []string) bool
}

// BuildTagFilter builds and compiles a new tag filter for the given inputs. All
// inputs are strings to be compiled to regular expressions and are mutually
// exclusive.
func BuildTagFilter(any, all string) (TagFilter, error) {
	// Ensure only one tag filter type is given.
	if any != "" && all != "" {
		return nil, fmt.Errorf("only one tag filter type may be specified")
	}

	switch {
	case any != "":
		re, err := regexp.Compile(any)
		if err != nil {
			return nil, fmt.Errorf("failed to compile tag_filter_any regular expression %q: %w", any, err)
		}
		return &TagFilterAny{re}, nil
	case all != "":
		re, err := regexp.Compile(all)
		if err != nil {
			return nil, fmt.Errorf("failed to compile tag_filter_all regular expression %q: %w", all, err)
		}
		return &TagFilterAll{re}, nil
	default:
		// If no filters were provided, return the null filter which just returns
		// false for all matches.
		return &TagFilterNull{}, nil
	}
}

var _ TagFilter = (*TagFilterNull)(nil)

// TagFilterNull always returns false.
type TagFilterNull struct{}

func (f *TagFilterNull) Matches(tags []string) bool {
	return false
}

func (f *TagFilterNull) Name() string {
	return "(none)"
}

// TagFilterAny filters based on the entire list. If any tag in the list
// matches, it returns true. If no tags match, it returns false.
type TagFilterAny struct {
	re *regexp.Regexp
}

func (f *TagFilterAny) Matches(tags []string) bool {
	if f.re == nil {
		return false
	}
	for _, t := range tags {
		if f.re.MatchString(t) {
			return true
		}
	}
	return false
}

func (f *TagFilterAny) Name() string {
	return fmt.Sprintf("any(%s)", f.re.String())
}

var _ TagFilter = (*TagFilterAll)(nil)

// TagFilterAll filters based on the entire list. If all tags in the last match,
// it returns true. If one more more tags do not match, it returns false.
type TagFilterAll struct {
	re *regexp.Regexp
}

func (f *TagFilterAll) Name() string {
	return fmt.Sprintf("all(%s)", f.re.String())
}

func (f *TagFilterAll) Matches(tags []string) bool {
	if f.re == nil {
		return false
	}
	for _, t := range tags {
		if !f.re.MatchString(t) {
			return false
		}
	}
	return true
}
