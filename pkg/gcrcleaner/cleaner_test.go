package gcrcleaner

import (
	"regexp"
	"testing"
	"time"

	gcrgoogle "github.com/google/go-containerregistry/pkg/v1/google"
)

func Test_shouldDelete(t *testing.T) {
	now := time.Now()
	yesterday := time.Now().AddDate(0, 0, -1)
	type args struct {
		m                 gcrgoogle.ManifestInfo
		since             time.Time
		allowTag          bool
		tagFilterRegexp   *regexp.Regexp
		tagFilterMatchAny bool
		excludedTags      map[string]struct{}
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "delete all untagged",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{},
				},
				since:             now,
				allowTag:          false,
				tagFilterRegexp:   nil,
				tagFilterMatchAny: false,
				excludedTags:      nil,
			},
			want: true,
		},
		{
			name: "delete none untagged older than 1 day",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   now,
					Uploaded:  now,
					Tags:      []string{},
				},
				since:             yesterday,
				allowTag:          false,
				tagFilterRegexp:   nil,
				tagFilterMatchAny: false,
				excludedTags:      nil,
			},
			want: false,
		},
		{
			name: "delete 1 untagged older than 1 day",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{},
				},
				since:             now,
				allowTag:          false,
				tagFilterRegexp:   nil,
				tagFilterMatchAny: false,
				excludedTags:      nil,
			},
			want: true,
		},
		{
			name: "delete none untagged for tag present",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{"tag"},
				},
				since:             now,
				allowTag:          false,
				tagFilterRegexp:   nil,
				tagFilterMatchAny: false,
				excludedTags:      nil,
			},
			want: false,
		},
		{
			name: "delete 1 tagged no filter",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{"tag"},
				},
				since:             now,
				allowTag:          true,
				tagFilterRegexp:   regexp.MustCompile(""),
				tagFilterMatchAny: false,
				excludedTags:      nil,
			},
			want: true,
		},
		{
			name: "delete none tagged for filter not matching",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{"tag"},
				},
				since:             now,
				allowTag:          true,
				tagFilterRegexp:   regexp.MustCompile("^ciccio$"),
				tagFilterMatchAny: false,
				excludedTags:      nil,
			},
			want: false,
		},
		{
			name: "delete 1 tagged for filter matching",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{"tag"},
				},
				since:             now,
				allowTag:          true,
				tagFilterRegexp:   regexp.MustCompile("^tag$"),
				tagFilterMatchAny: false,
				excludedTags:      nil,
			},
			want: true,
		},
		{
			name: "delete none tagged for no filter but tag excluded",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{"tag"},
				},
				since:             now,
				allowTag:          true,
				tagFilterRegexp:   regexp.MustCompile(""),
				tagFilterMatchAny: false,
				excludedTags:      map[string]struct{}{"tag": struct{}{}},
			},
			want: false,
		},
		{
			name: "delete none tagged for filter not matching and tag excluded",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{"tag"},
				},
				since:             now,
				allowTag:          true,
				tagFilterRegexp:   regexp.MustCompile("^ciccio$"),
				tagFilterMatchAny: false,
				excludedTags:      map[string]struct{}{"tag": struct{}{}},
			},
			want: false,
		},
		{
			name: "delete none tagged for filter matching but tag excluded",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{"tag"},
				},
				since:             now,
				allowTag:          true,
				tagFilterRegexp:   regexp.MustCompile("^tag$"),
				tagFilterMatchAny: false,
				excludedTags:      map[string]struct{}{"tag": struct{}{}},
			},
			want: false,
		},
		{
			name: "delete 1 tagged for filter matching and tag not excluded",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{"tag"},
				},
				since:             now,
				allowTag:          true,
				tagFilterRegexp:   regexp.MustCompile("^tag$"),
				tagFilterMatchAny: false,
				excludedTags:      map[string]struct{}{"ciccio": struct{}{}},
			},
			want: true,
		},
		{
			name: "delete none tagged for no tags excluded, but filter matching only one tag and tagFilterMatchAny false",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{"tag1", "tag2"},
				},
				since:             now,
				allowTag:          true,
				tagFilterRegexp:   regexp.MustCompile("^tag1$"),
				tagFilterMatchAny: false,
				excludedTags:      map[string]struct{}{"ciccio": struct{}{}},
			},
			want: false,
		},
		{
			name: "delete 1 tagged for no tags excluded, filter matching only one tag and tagFilterMatchAny true",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{"tag1", "tag2"},
				},
				since:             now,
				allowTag:          true,
				tagFilterRegexp:   regexp.MustCompile("^tag1$"),
				tagFilterMatchAny: true,
				excludedTags:      map[string]struct{}{"ciccio": struct{}{}},
			},
			want: true,
		},
		{
			name: "delete 1 tagged for no tags excluded, filter matching all tags, tagFilterMatchAny false",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{"tag1", "tag2"},
				},
				since:             now,
				allowTag:          true,
				tagFilterRegexp:   regexp.MustCompile("^tag.*$"),
				tagFilterMatchAny: false,
				excludedTags:      map[string]struct{}{"ciccio": struct{}{}},
			},
			want: true,
		},
		{
			name: "delete 1 tagged for no tags excluded, filter matching all tags, tagFilterMatchAny true",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{"tag1", "tag2"},
				},
				since:             now,
				allowTag:          true,
				tagFilterRegexp:   regexp.MustCompile("^tag.*$"),
				tagFilterMatchAny: true,
				excludedTags:      map[string]struct{}{"ciccio": struct{}{}},
			},
			want: true,
		},
		{
			name: "delete none tagged for filter matching all tags, tagFilterMatchAny false, but first tag excluded",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{"tag1", "tag2"},
				},
				since:             now,
				allowTag:          true,
				tagFilterRegexp:   regexp.MustCompile("^tag.*$"),
				tagFilterMatchAny: false,
				excludedTags:      map[string]struct{}{"tag1": struct{}{}},
			},
			want: false,
		},
		{
			name: "delete none tagged for filter matching all tags, tagFilterMatchAny false, but second tag excluded",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{"tag1", "tag2"},
				},
				since:             now,
				allowTag:          true,
				tagFilterRegexp:   regexp.MustCompile("^tag.*$"),
				tagFilterMatchAny: false,
				excludedTags:      map[string]struct{}{"tag2": struct{}{}},
			},
			want: false,
		},
		{
			name: "delete none tagged for filter matching all tags, tagFilterMatchAny true, but first tag excluded",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{"tag1", "tag2"},
				},
				since:             now,
				allowTag:          true,
				tagFilterRegexp:   regexp.MustCompile("^tag.*$"),
				tagFilterMatchAny: true,
				excludedTags:      map[string]struct{}{"tag1": struct{}{}},
			},
			want: false,
		},
		{
			name: "delete none tagged for filter matching all tags, tagFilterMatchAny true, but second tag excluded",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{"tag1", "tag2"},
				},
				since:             now,
				allowTag:          true,
				tagFilterRegexp:   regexp.MustCompile("^tag.*$"),
				tagFilterMatchAny: true,
				excludedTags:      map[string]struct{}{"tag2": struct{}{}},
			},
			want: false,
		},
		{
			name: "delete none tagged for filter matching all tags, tagFilterMatchAny false, but all tags excluded",
			args: args{
				m: gcrgoogle.ManifestInfo{
					Size:      0,
					MediaType: "",
					Created:   yesterday,
					Uploaded:  yesterday,
					Tags:      []string{"tag1", "tag2"},
				},
				since:             now,
				allowTag:          true,
				tagFilterRegexp:   regexp.MustCompile("^tag.*$"),
				tagFilterMatchAny: false,
				excludedTags: map[string]struct{}{
					"tag1": struct{}{},
					"tag2": struct{}{},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldDelete(tt.args.m, tt.args.since, tt.args.allowTag, tt.args.tagFilterRegexp, tt.args.tagFilterMatchAny, tt.args.excludedTags); got != tt.want {
				t.Errorf("shouldDelete() = %v, want %v", got, tt.want)
			}
		})
	}
}
