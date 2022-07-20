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

package gcrcleaner

import (
	"fmt"
	"testing"
)

func TestErrsToError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   []error
		exp  string
	}{
		{
			name: "empty",
			in:   nil,
			exp:  "<nil>",
		},
		{
			name: "single",
			in:   []error{fmt.Errorf("oops")},
			exp:  "oops",
		},
		{
			name: "multi",
			in: []error{
				fmt.Errorf("oops"),
				fmt.Errorf("i"),
				fmt.Errorf("did"),
				fmt.Errorf("it"),
				fmt.Errorf("again"),
			},
			exp: `5 errors occurred:
  * again
  * did
  * i
  * it
  * oops
`,
		},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got, want := fmt.Sprintf("%v", ErrsToError(tc.in)), tc.exp; got != want {
				t.Errorf("expected \n%q\nto be\n%q\n", got, want)
			}
		})
	}
}
