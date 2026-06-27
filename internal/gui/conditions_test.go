package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiffeek/hyprdynamicmonitors/internal/config"
	"github.com/fiffeek/hyprdynamicmonitors/internal/utils"
)

const sampleConfig = `# My hyprdynamicmonitors config
[general]
destination = "$HOME/.config/hypr/monitors.conf"

[power_events]
[power_events.dbus_query_object]
destination = "org.freedesktop.UPower"

# The docked profile
[profiles.Docked]
config_file = "hyprconfigs/Docked.go.tmpl"
config_file_type = "template"
[profiles.Docked.conditions]
[[profiles.Docked.conditions.required_monitors]]
description = "Dell U2720Q"
monitor_tag = "monitor0"

[profiles.Portable]
config_file = "hyprconfigs/Portable.go.tmpl"
config_file_type = "template"
[profiles.Portable.conditions]
[[profiles.Portable.conditions.required_monitors]]
name = "eDP-1"
`

// writeTemp writes content to a temp config file and returns its path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	// The profile config_file paths are relative; create the dir they point to so
	// validation (which absolutizes them) is happy.
	if err := os.MkdirAll(filepath.Join(dir, "hyprconfigs"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Docked", "Portable"} {
		if err := os.WriteFile(filepath.Join(dir, "hyprconfigs", name+".go.tmpl"), []byte("monitor=...\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRewriteProfileConditions_EditsOnlyTargetBlock(t *testing.T) {
	path := writeTemp(t, sampleConfig)

	raw, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// New conditions: match by name with regex + AC power.
	cond := &config.ProfileCondition{
		RequiredMonitors: []*config.RequiredMonitor{
			{Name: utils.StringPtr("DP-.*"), MatchNameUsingRegex: utils.BoolPtr(true), MonitorTag: utils.StringPtr("ext")},
		},
		PowerState: utils.JustPtr(config.AC),
	}

	updated, err := rewriteProfileConditions(sampleConfig, raw.Profiles["Docked"], "Docked", cond)
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	// Untouched bits must survive verbatim.
	for _, must := range []string{
		"# My hyprdynamicmonitors config",
		"[general]",
		`destination = "$HOME/.config/hypr/monitors.conf"`,
		"[power_events.dbus_query_object]",
		"# The docked profile",
		"[profiles.Portable]",
		`name = "eDP-1"`,
		`config_file = "hyprconfigs/Docked.go.tmpl"`, // original relative path preserved
	} {
		if !strings.Contains(updated, must) {
			t.Errorf("expected updated config to still contain %q\n---\n%s", must, updated)
		}
	}

	// The old Docked condition must be gone, the new one present.
	if strings.Contains(updated, "Dell U2720Q") {
		t.Errorf("old condition should have been replaced\n---\n%s", updated)
	}
	if !strings.Contains(updated, `name = "DP-.*"`) || !strings.Contains(updated, "match_name_using_regex = true") {
		t.Errorf("new condition missing\n---\n%s", updated)
	}

	// Re-write to disk and reload to confirm it parses and is semantically correct.
	out := writeTemp(t, updated)
	reloaded, err := config.Load(out)
	if err != nil {
		t.Fatalf("reload of rewritten config failed: %v\n---\n%s", err, updated)
	}

	docked := reloaded.Profiles["Docked"]
	if docked == nil || docked.Conditions == nil || len(docked.Conditions.RequiredMonitors) != 1 {
		t.Fatalf("Docked conditions not as expected: %+v", docked)
	}
	rm := docked.Conditions.RequiredMonitors[0]
	if rm.Name == nil || *rm.Name != "DP-.*" {
		t.Errorf("expected name DP-.*, got %+v", rm.Name)
	}
	if docked.Conditions.PowerState == nil || *docked.Conditions.PowerState != config.AC {
		t.Errorf("expected AC power state, got %+v", docked.Conditions.PowerState)
	}
	// Portable must be entirely intact.
	portable := reloaded.Profiles["Portable"]
	if portable == nil || portable.Conditions == nil || len(portable.Conditions.RequiredMonitors) != 1 ||
		portable.Conditions.RequiredMonitors[0].Name == nil || *portable.Conditions.RequiredMonitors[0].Name != "eDP-1" {
		t.Errorf("Portable profile was altered: %+v", portable)
	}
}

func TestProfileBlockRange_LastProfile(t *testing.T) {
	lines := strings.Split(sampleConfig, "\n")
	start, end := profileBlockRange(lines, "Portable")
	if start < 0 {
		t.Fatal("Portable block not found")
	}
	block := strings.Join(lines[start:end], "\n")
	if !strings.Contains(block, "[profiles.Portable]") || strings.Contains(block, "Docked") {
		t.Errorf("unexpected Portable block:\n%s", block)
	}
}
