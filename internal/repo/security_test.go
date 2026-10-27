package repo

import (
	"strings"
	"testing"
)

func TestRedactInputRemovesCredentialsAndQuery(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "https://alice:secret@example.invalid/repo.git?token=secret#frag", want: "https://<credentials-redacted>@example.invalid/repo.git"},
		{input: "git@github.com:owner/repo.git", want: "git@<credentials-redacted>:owner/repo.git"},
		{input: "https://user:secret@example.invalid/x", want: "https://<credentials-redacted>@example.invalid/x"},
		{input: "https://example.invalid/repo@v2", want: "https://example.invalid/repo@v2"},
		{input: "https://example.invalid/repo?ref=a@b", want: "https://example.invalid/repo"},
		{input: "https://user:secret@example.invalid/repo@v2", want: "https://<credentials-redacted>@example.invalid/repo@v2"},
	}
	for _, test := range tests {
		got := redactInput(test.input)
		if got != test.want {
			t.Errorf("redactInput(%q) = %q, want %q", test.input, got, test.want)
		}
		if strings.Contains(got, "secret") {
			t.Errorf("redacted input leaked credential: %q", got)
		}
	}
}
