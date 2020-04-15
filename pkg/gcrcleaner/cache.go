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

// Cache is an interface used by the PubSub() function to prevent duplicate
// messages from being processed.
type Cache interface {
	// Insert inserts the item into the cache. If the item already exists, this
	// method returns true.
	Insert(string) bool

	// Stop stops the cache. When Stop returns, the cache must not perform any
	// additionally processing.
	Stop()
}
