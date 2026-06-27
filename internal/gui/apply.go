package gui

import (
	"fmt"
	"os/exec"

	"github.com/fiffeek/hyprdynamicmonitors/internal/tui"
	"github.com/sirupsen/logrus"
)

// onApplyClicked applies the current in-memory layout to Hyprland live, using
// the exact same `hyprctl keyword monitor` mechanism as the TUI.
func (a *App) onApplyClicked() {
	// Validate first (ConvertToHyprMonitors runs each monitor's Validate()).
	if _, err := tui.ConvertToHyprMonitors(a.monitors); err != nil {
		a.setError(fmt.Errorf("invalid layout: %w", err))
		return
	}

	var failed int
	var lastErr error
	for _, m := range a.monitors {
		cmd := fmt.Sprintf("hyprctl keyword monitor %q", m.ToHypr())
		// nolint:gosec
		if err := exec.Command("sh", "-c", cmd).Run(); err != nil {
			failed++
			lastErr = err
			logrus.WithError(err).WithField("monitor", m.Name).Error("cant apply hypr settings")
		}
	}

	if failed > 0 {
		a.setError(fmt.Errorf("failed to apply %d monitor(s): %w", failed, lastErr))
		return
	}
	a.setStatus("Applied %d monitor(s) to Hyprland", len(a.monitors))
}
