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
	Matches(tags []string) bool
}

// BuildTagFilter builds and compiles a new tag filter for the given inputs. All
// inputs are strings to be compiled to regular expressions and are mutually
// exclusive.
func BuildTagFilter(first, any, all string) (TagFilter, error) {
	// Ensure only one tag filter type is given.
	if (first != "" && any != "") || (first != "" && all != "") || (any != "" && all != "") {
		return nil, fmt.Errorf("only one tag filter type may be specified")
	}

	switch {
	case first != "":
		re, err := regexp.Compile(first)
		if err != nil {
			return nil, fmt.Errorf("failed to compile tag_filter regular expression %q: %w", first, err)
		}
		return &TagFilterFirst{re}, nil
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
		// false for all matches. This preserves the "allow_tagged" behavior.
		return &TagFilterNull{}, nil
	}
}

var _ TagFilter = (*TagFilterNull)(nil)

// TagFilterNull always returns false.
type TagFilterNull struct{}

func (f *TagFilterNull) Matches(tags []string) bool {
	return false
}

var _ TagFilter = (*TagFilterFirst)(nil)

// TagFilterFirst filters based on the first item in the list. If the list is
// empty or if the first item does not match the regex, it returns false.
type TagFilterFirst struct {
	re *regexp.Regexp
}

func (f *TagFilterFirst) Matches(tags []string) bool {
	if len(tags) < 1 || f.re == nil {
		return false
	}
	return f.re.MatchString(tags[0])
}

var _ TagFilter = (*TagFilterAny)(nil)

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

var _ TagFilter = (*TagFilterAll)(nil)

// TagFilterAll filters based on the entire list. If all tags in the last match,
// it returns true. If one more more tags do not match, it returns false.
type TagFilterAll struct {
	re *regexp.Regexp
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
