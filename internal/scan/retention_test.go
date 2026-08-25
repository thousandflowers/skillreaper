package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSettings(t *testing.T, dir, base, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, base), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRetentionDays(t *testing.T) {
	tests := []struct {
		name         string
		files        map[string]string
		wantDays     int
		wantExplicit bool
	}{
		{
			name:     "no settings at all",
			wantDays: DefaultCleanupPeriodDays,
		},
		{
			name:     "settings without the key",
			files:    map[string]string{"settings.json": `{"hooks":{}}`},
			wantDays: DefaultCleanupPeriodDays,
		},
		{
			name:         "explicit value",
			files:        map[string]string{"settings.json": `{"cleanupPeriodDays": 365}`},
			wantDays:     365,
			wantExplicit: true,
		},
		{
			// settings.local.json layers over settings.json in Claude Code, so
			// it has to win here too or the reported horizon is the wrong one.
			name: "local overrides base",
			files: map[string]string{
				"settings.json":       `{"cleanupPeriodDays": 30}`,
				"settings.local.json": `{"cleanupPeriodDays": 180}`,
			},
			wantDays:     180,
			wantExplicit: true,
		},
		{
			// Claude Code rejects 0 as a validation error rather than reading
			// it as "never sweep", so a 0 on disk is a broken config and must
			// not be reported as an infinite horizon.
			name:     "zero is not infinite retention",
			files:    map[string]string{"settings.json": `{"cleanupPeriodDays": 0}`},
			wantDays: DefaultCleanupPeriodDays,
		},
		{
			name:     "malformed json falls back",
			files:    map[string]string{"settings.json": `{not json`},
			wantDays: DefaultCleanupPeriodDays,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for base, body := range tt.files {
				writeSettings(t, dir, base, body)
			}
			days, explicit := RetentionDays(dir)
			if days != tt.wantDays {
				t.Errorf("days = %d, want %d", days, tt.wantDays)
			}
			if explicit != tt.wantExplicit {
				t.Errorf("explicit = %v, want %v", explicit, tt.wantExplicit)
			}
		})
	}
}
