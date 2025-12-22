package main

import "testing"

func TestGithubDeployKeyURL(t *testing.T) {
	tests := []struct {
		name     string
		repoURL  string
		expected string
	}{
		{
			name:     "SSH format",
			repoURL:  "git@github.com:jonnyzzz/stevedore.git",
			expected: "https://github.com/jonnyzzz/stevedore/settings/keys",
		},
		{
			name:     "SSH format without .git",
			repoURL:  "git@github.com:owner/repo",
			expected: "https://github.com/owner/repo/settings/keys",
		},
		{
			name:     "SSH URL format",
			repoURL:  "ssh://git@github.com/owner/repo.git",
			expected: "https://github.com/owner/repo/settings/keys",
		},
		{
			name:     "HTTPS format",
			repoURL:  "https://github.com/owner/repo.git",
			expected: "https://github.com/owner/repo/settings/keys",
		},
		{
			name:     "HTTPS format without .git",
			repoURL:  "https://github.com/owner/repo",
			expected: "https://github.com/owner/repo/settings/keys",
		},
		{
			name:     "Non-GitHub SSH URL",
			repoURL:  "git@gitlab.com:owner/repo.git",
			expected: "",
		},
		{
			name:     "Non-GitHub HTTPS URL",
			repoURL:  "https://gitlab.com/owner/repo.git",
			expected: "",
		},
		{
			name:     "Empty URL",
			repoURL:  "",
			expected: "",
		},
		{
			name:     "Whitespace",
			repoURL:  "  git@github.com:owner/repo.git  ",
			expected: "https://github.com/owner/repo/settings/keys",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := githubDeployKeyURL(tt.repoURL)
			if result != tt.expected {
				t.Errorf("githubDeployKeyURL(%q) = %q, want %q", tt.repoURL, result, tt.expected)
			}
		})
	}
}
