package gcrcleaner

import (
	"strings"

	"golang.org/x/exp/slices"
)

type LiteralFilter struct {
	raw        string
	tagsToKeep []string
}

func (l *LiteralFilter) Matches(tags []string) bool {
	for _, tag := range tags {
		if slices.Contains(l.tagsToKeep, tag) {
			return false
		}
	}
	return true
}

func (l *LiteralFilter) Name() string {
	return l.raw
}

func BuildLiteralFilter(literal string) TagFilter {
	return &LiteralFilter{
		raw:        literal,
		tagsToKeep: strings.Split(literal, ","),
	}
}
