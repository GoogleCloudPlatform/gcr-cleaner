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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	contentTypeHeader = "Content-Type"
	contentTypeJSON   = "application/json"
)

// Server is a cleaning server.
type Server struct {
	cleaner *Cleaner
}

// NewServer creates a new server for handler functions.
func NewServer(cleaner *Cleaner) (*Server, error) {
	if cleaner == nil {
		return nil, fmt.Errorf("missing cleaner")
	}

	return &Server{
		cleaner: cleaner,
	}, nil
}

// PubSubHandler is an http handler that invokes the cleaner from a pubsub
// request. Unlike an HTTP request, the pubsub endpoint always returns a success
// unless the pubsub message is malformed.
func (s *Server) PubSubHandler(cache Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var m pubsubMessage
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			err = fmt.Errorf("failed to decode pubsub message: %w", err)
			s.handleError(w, err, 400)
			return
		}

		// PubSub is "at least once" delivery. The cleaner is idempotent, but
		// let's try to prevent unnecessary work by not processing messages we've
		// already received.
		msgID := m.Subscription + "/" + m.Message.ID
		if exists := cache.Insert(msgID); exists {
			log.Printf("already processed message %s", msgID)
			w.WriteHeader(204)
			return
		}

		if len(m.Message.Data) == 0 {
			err := fmt.Errorf("missing data in pubsub payload")
			s.handleError(w, err, 400)
			return
		}

		// Start a goroutine to delete the images
		body := ioutil.NopCloser(bytes.NewReader(m.Message.Data))
		go func() {
			if _, _, err := s.clean(body); err != nil {
				log.Printf("error async: %s", err.Error())
			}
		}()

		w.WriteHeader(204)
	}
}

// HTTPHandler is an http handler that invokes the cleaner with the given
// parameters.
func (s *Server) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deleted, status, err := s.clean(r.Body)
		if err != nil {
			s.handleError(w, err, status)
			return
		}

		b, err := json.Marshal(&cleanResp{
			Count: len(deleted),
			Refs:  deleted,
		})
		if err != nil {
			err = fmt.Errorf("failed to marshal JSON errors: %w", err)
			s.handleError(w, err, 500)
			return
		}

		w.WriteHeader(200)
		w.Header().Set(contentTypeHeader, contentTypeJSON)
		fmt.Fprint(w, string(b))
	}
}

// clean reads the given body as JSON and starts a cleaner instance.
func (s *Server) clean(r io.ReadCloser) ([]manifest, int, error) {
	var p Payload
	if err := json.NewDecoder(r).Decode(&p); err != nil {
		return nil, 500, fmt.Errorf("failed to decode payload as JSON: %w", err)
	}

	repo := p.Repo

	// Convert duration to a negative value, since we're about to "add" it to the
	// since time.
	sub := time.Duration(p.Grace)
	if p.Grace > 0 {
		sub = sub * -1
	}

	since := time.Now().UTC().Add(sub)
	allowTagged := p.AllowTagged
	keep := p.Keep
	tagFilterRegexp, err := regexp.Compile(p.TagFilter)
	if err != nil {
		return nil, 500, fmt.Errorf("failed to parse tag_filter: %w", err)
	}
	tagFilterMatchAny := p.TagFilterMatchAny
	excludedTags := map[string]struct{}{}
	if p.ExcludedTags != "" {
		for _, v := range strings.Split(p.ExcludedTags, ",") {
			excludedTags[v] = struct{}{}
		}
	}
	dryRun := p.DryRun

	log.Printf("deleting refs for %s since %s\n", repo, since)

	deleted, err := s.cleaner.Clean(repo, since, allowTagged, keep, tagFilterRegexp, tagFilterMatchAny, excludedTags, dryRun)
	if err != nil {
		return nil, 400, fmt.Errorf("failed to clean: %w", err)
	}

	log.Printf("deleted %d refs for %s", len(deleted), repo)

	return deleted, 200, nil
}

// handleError returns a JSON-formatted error message
func (s *Server) handleError(w http.ResponseWriter, err error, status int) {
	log.Printf("error %d: %s", status, err.Error())

	b, err := json.Marshal(&errorResp{Error: err.Error()})
	if err != nil {
		err = fmt.Errorf("failed to marshal JSON errors: %w", err)
		http.Error(w, err.Error(), 500)
		return
	}

	w.WriteHeader(status)
	w.Header().Set(contentTypeHeader, contentTypeJSON)
	fmt.Fprint(w, string(b))
}

// Payload is the expected incoming payload format.
type Payload struct {
	// Repo is the name of the repo in the format gcr.io/foo/bar
	Repo string `json:"repo"`

	// Grace is a time.Duration value indicating how much grade period should be
	// given to new, untagged layers. The default is no grace.
	Grace duration `json:"grace"`

	// AllowTagged is a Boolean value determine if tagged images are allowed
	// to be deleted.
	AllowTagged bool `json:"allow_tagged"`

	// Keep is the minimum number of images to keep.
	Keep int `json:"keep"`

	// TagFilter is the tags pattern to be allowed removing
	TagFilter string `json:"tag_filter"`

	// TagFilterMatchAny is a Boolean value to determine if all tags on one image should
	// match the TagFilter (false) or at least one (true)
	TagFilterMatchAny bool `json:"tag_filter_match_any"`

	// ExcludedTags is the comma separated list of tags to exclude
	ExcludedTags string `json:"excluded_tags"`

	// DryRun is a Boolean value to enable Dry Run
	DryRun bool `json:"dry_run"`
}

type pubsubMessage struct {
	Message struct {
		Data []byte `json:"data"`
		ID   string `json:"message_id"`
	} `json:"message"`
	Subscription string `json:"subscription"`
}

type cleanResp struct {
	Count int        `json:"count"`
	Refs  []manifest `json:"refs"`
}

type errorResp struct {
	Error string `json:"error"`
}

type duration time.Duration

func (d duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}

	switch val := v.(type) {
	case float64:
		*d = duration(time.Duration(val))
		return nil
	case string:
		s, err := time.ParseDuration(val)
		if err != nil {
			return err
		}
		*d = duration(s)
		return nil
	default:
		return fmt.Errorf("invalid duration type %T", val)
	}
}
