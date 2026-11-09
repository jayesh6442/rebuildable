package checks

import (
	"context"
	"strings"
	"testing"

	"github.com/jayeshsarvaiya/rebuildable/internal/repo"
)

func TestGitHubTokenFormats(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		body   string
	}{
		{"classic personal access token", "ghp_", strings.Repeat("A", 36)},
		{"OAuth access token", "gho_", strings.Repeat("B", 36)},
		{"user-to-server token", "ghu_", strings.Repeat("C", 36)},
		{"server-to-server token", "ghs_", strings.Repeat("D", 32)},
		{"refresh token", "ghr_", strings.Repeat("E", 36)},
		{"fine-grained personal access token", "github_pat_", strings.Repeat("F_", 16)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			secret := tc.prefix + tc.body
			loaded := loadContentRepo(t, map[string]string{
				"config.toml": "token = \"" + secret + "\"\n",
			}, repo.EcosystemGo)
			result := (environmentCheck{}).Run(context.Background(), loaded)
			if result.Status != StatusFail || len(result.Findings) != 1 ||
				result.Findings[0].File != "config.toml" ||
				result.Findings[0].Message != "hard-coded secret pattern detected" {
				t.Errorf("token %q: status = %s, findings = %+v", tc.name, result.Status, result.Findings)
			}
			for _, finding := range result.Findings {
				if strings.Contains(finding.Message, secret) || strings.Contains(finding.Hint, secret) {
					t.Errorf("finding exposed the token: %+v", finding)
				}
			}
		})
	}
}

func TestGitHubTokenPatternRejectsOrdinaryText(t *testing.T) {
	for _, input := range []string{
		"this is a GitHub issue without a credential",
		"ghx_" + strings.Repeat("A", 40),
		"gho_" + strings.Repeat("A", 20),
		"github_pat_" + strings.Repeat("B", 18),
		"prefix_ghp_" + strings.Repeat("C", 36),
	} {
		if hasSecret(input) {
			t.Errorf("false positive for %q", input)
		}
	}
}
