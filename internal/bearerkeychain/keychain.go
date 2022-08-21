// Copyright 2022 The GCR Cleaner Authors
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

package bearerkeychain

import (
	gcrauthn "github.com/google/go-containerregistry/pkg/authn"
)

// Keychain represents a Bearer Token keychain entry.
type Keychain struct {
	token string
}

// New creates a new Bearer Token keychain. If the provided token is empty, this
// will always resolve to anonymous auth. Otherwise it returns the bearer auth.
func New(token string) *Keychain {
	return &Keychain{
		token: token,
	}
}

// Resolve implements Resolver for the given keychain.
func (k *Keychain) Resolve(_ gcrauthn.Resource) (gcrauthn.Authenticator, error) {
	if k.token == "" {
		return gcrauthn.Anonymous, nil
	}
	return &gcrauthn.Bearer{Token: k.token}, nil
}
