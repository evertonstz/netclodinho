package webhook

import (
	"testing"

	"github.com/google/go-github/v68/github"
)

func TestRepoOwnerAndName(t *testing.T) {
	tests := []struct {
		name      string
		repo      *github.Repository
		wantOwner string
		wantRepo  string
	}{
		{
			name:      "full name",
			repo:      &github.Repository{FullName: github.Ptr("angristan/netclode")},
			wantOwner: "angristan",
			wantRepo:  "netclode",
		},
		{
			name:      "org repo",
			repo:      &github.Repository{FullName: github.Ptr("my-org/my-repo")},
			wantOwner: "my-org",
			wantRepo:  "my-repo",
		},
		{
			name: "fallback to owner and name fields",
			repo: &github.Repository{
				Owner: &github.User{Login: github.Ptr("owner")},
				Name:  github.Ptr("repo"),
			},
			wantOwner: "owner",
			wantRepo:  "repo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo := repoOwnerAndName(tt.repo)
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("repoOwnerAndName() = (%q, %q), want (%q, %q)", owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}
