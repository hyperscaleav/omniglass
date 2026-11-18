package releaseversion

import (
	"strings"
	"testing"
)

func TestNext(t *testing.T) {
	cases := []struct {
		name    string
		req     Request
		want    Version
		wantErr string
	}{
		{
			name: "first candidate on a new branch",
			req:  Request{Branch: "release/0.1", Tags: []string{"v0.0.0"}},
			want: Version{Tag: "v0.1.0-rc.1", Prerelease: true, Previous: "v0.0.0"},
		},
		{
			name: "next candidate after a cherry-pick",
			req: Request{
				Branch: "release/0.1",
				Tags:   []string{"v0.0.0", "v0.1.0-rc.1", "v0.1.0-rc.2"},
			},
			want: Version{Tag: "v0.1.0-rc.3", Prerelease: true, Previous: "v0.0.0"},
		},
		{
			name: "final on the newest candidate",
			req: Request{
				Branch:   "release/0.1",
				Tags:     []string{"v0.0.0", "v0.1.0-rc.1", "v0.1.0-rc.2"},
				HeadTags: []string{"v0.1.0-rc.2"},
				Final:    true,
			},
			want: Version{Tag: "v0.1.0", Prerelease: false, Previous: "v0.0.0"},
		},
		{
			name: "a candidate after a final is the next patch",
			req: Request{
				Branch: "release/0.1",
				Tags:   []string{"v0.0.0", "v0.1.0-rc.1", "v0.1.0"},
			},
			want: Version{Tag: "v0.1.1-rc.1", Prerelease: true, Previous: "v0.1.0"},
		},
		{
			name: "another branch's tags do not count",
			req: Request{
				Branch: "release/0.2",
				Tags:   []string{"v0.0.0", "v0.1.0", "v0.1.1", "v0.1.2-rc.1", "v1.0.0"},
			},
			want: Version{Tag: "v0.2.0-rc.1", Prerelease: true, Previous: "v0.1.1"},
		},
		{
			name: "tags that are not versions are ignored",
			req:  Request{Branch: "release/0.1", Tags: []string{"latest", "v0.1", "v0.1.0-beta.1", "soak-abc123"}},
			want: Version{Tag: "v0.1.0-rc.1", Prerelease: true},
		},
		{
			name:    "a branch that is not a release branch",
			req:     Request{Branch: "main"},
			wantErr: "not a release branch",
		},
		{
			name:    "a release branch without a minor",
			req:     Request{Branch: "release/1"},
			wantErr: "not a release branch",
		},
		{
			name: "re-cutting a commit already tagged as a candidate",
			req: Request{
				Branch:   "release/0.1",
				Tags:     []string{"v0.1.0-rc.1"},
				HeadTags: []string{"v0.1.0-rc.1"},
			},
			wantErr: "already v0.1.0-rc.1",
		},
		{
			name:    "a final with no candidate at all",
			req:     Request{Branch: "release/0.1", Tags: []string{"v0.0.0"}, Final: true},
			wantErr: "no candidate",
		},
		{
			name: "a final on a commit that is not the newest candidate",
			req: Request{
				Branch:   "release/0.1",
				Tags:     []string{"v0.1.0-rc.1", "v0.1.0-rc.2"},
				HeadTags: []string{"v0.1.0-rc.1"},
				Final:    true,
			},
			wantErr: "newest candidate v0.1.0-rc.2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Next(tc.req)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Next() error = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Next() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("Next() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
