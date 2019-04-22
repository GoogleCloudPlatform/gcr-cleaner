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
	"encoding/json"
	"fmt"

	"cloud.google.com/go/compute/metadata"
	gcrauthn "github.com/google/go-containerregistry/pkg/authn"
	"github.com/pkg/errors"
)

// TokenProvider is an interface which provides an oauth2 access token.
type TokenProvider interface {
	Token() (string, error)
}

// TokenProviderFunc is a function that returns a bearer token that satisfies
// the TokenProvider interface.
type TokenProviderFunc func() (string, error)

// Token implements TokenProvider.
func (f TokenProviderFunc) Token() (string, error) {
	return f()
}

// TokenProviderFromString returns a TokenProvider that returns the given string
// as the token.
func TokenProviderFromString(s string) TokenProviderFunc {
	return func() (string, error) {
		return s, nil
	}
}

// TokenProviderMetadataServer returns a token provider that retrieves the
// oauth2 bearer token from the instance metadata service on GCP.
func TokenProviderMetadataServer() TokenProviderFunc {
	return func() (string, error) {
		resp, err := metadata.Get("instance/service-accounts/default/token")
		if err != nil {
			return "", errors.Wrap(err, "failed to read metadata")
		}

		var t struct {
			Token string `json:"access_token"`
		}

		if err := json.Unmarshal([]byte(resp), &t); err != nil {
			return "", errors.Wrap(err, "failed to parse token as JSON")
		}

		if t.Token == "" {
			return "", errors.New("token not found")
		}

		return fmt.Sprintf("Bearer %s", t.Token), nil
	}
}

// authentifactorFunc is an internal wrapper around authn.Authenticator.
type authenticatorFunc func() (string, error)

// Authorization implements authn.Authenticator.
func (f authenticatorFunc) Authorization() (string, error) {
	return f()
}

// bearerAuthenticator is an internal func to convert a TokenProvider to an
// authenticator.
func bearerAuthenticator(t TokenProvider) gcrauthn.Authenticator {
	return authenticatorFunc(func() (string, error) {
		return t.Token()
	})
}
