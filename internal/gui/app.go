// Package gui implements a GTK4 graphical interface for hyprdynamicmonitors.
//
// It is inspired by wdisplays: monitors are shown as draggable rectangles on a
// canvas (left), per-monitor settings live in a side panel (right), and changes
// can be applied live to Hyprland. On top of plain display configuration it adds
// hyprdynamicmonitors profile management: the current monitor layout can be frozen
// into a named profile, reusing the exact same logic as the TUI.
package gui

import (
	"context"
	"fmt"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/fiffeek/hyprdynamicmonitors/internal/config"
	"github.com/fiffeek/hyprdynamicmonitors/internal/hypr"
	"github.com/fiffeek/hyprdynamicmonitors/internal/profilemaker"
	"github.com/fiffeek/hyprdynamicmonitors/internal/tui"
	"github.com/sirupsen/logrus"
)

const appID = "com.filipmikina.hyprdynamicmonitors"

// App is the top-level GTK application state.
type App struct {
	ctx     context.Context
	version string

	// Business logic reused verbatim from the rest of the project.
	cfg          *config.Config // may be nil if no valid config is found
	ipc          *hypr.IPC      // may be nil when not reachable (then apply is disabled)
	profileMaker *profilemaker.Service

	// Editing model. We reuse the TUI's MonitorSpec so that ToHypr / scaling /
	// transform logic stays in a single place.
	monitors []*tui.MonitorSpec
	selected int // index into monitors, -1 == nothing selected

	// Widgets.
	gtkApp      *gtk.Application
	win         *gtk.ApplicationWindow
	canvas      *Canvas
	sidebar     *Sidebar
	profileBar  *ProfileBar
	statusLabel *gtk.Label
	applyButton *gtk.Button
}

// NewApp wires the business logic (config, Hyprland IPC, profile maker) and
// snapshots the currently connected monitors into the editing model.
func NewApp(ctx context.Context, configPath, version string) (*App, error) {
	a := &App{
		ctx:      ctx,
		version:  version,
		selected: -1,
	}

	// Config is optional: without it the canvas + live apply still work, but
	// profile creation is disabled.
	cfg, err := config.NewConfig(configPath)
	if err != nil {
		logrus.WithError(err).Warn("cant read config; profile features will be disabled")
	} else {
		a.cfg = cfg
	}

	ipc, err := hypr.NewIPC(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Hyprland IPC: %w", err)
	}
	a.ipc = ipc
	a.profileMaker = profilemaker.NewService(cfg, ipc)

	monitors := ipc.GetConnectedMonitors()
	if err := monitors.Validate(); err != nil {
		return nil, fmt.Errorf("failed to get valid monitor information: %w", err)
	}
	a.setMonitors(monitors)

	return a, nil
}

// setMonitors converts raw Hyprland specs into the editable TUI model.
func (a *App) setMonitors(monitors hypr.MonitorSpecs) {
	a.monitors = a.monitors[:0]
	for _, m := range monitors {
		a.monitors = append(a.monitors, tui.NewMonitorSpec(m))
	}
	if len(a.monitors) > 0 && a.selected < 0 {
		a.selected = 0
	}
	if a.selected >= len(a.monitors) {
		a.selected = len(a.monitors) - 1
	}
}

// Run starts the GTK main loop. Blocks until the window is closed.
func (a *App) Run() error {
	a.gtkApp = gtk.NewApplication(appID, 0)
	a.gtkApp.ConnectActivate(a.activate)

	if code := a.gtkApp.Run(nil); code != 0 {
		return fmt.Errorf("gtk application exited with code %d", code)
	}
	return nil
}

// activate builds the whole window. Called once by GTK on startup.
func (a *App) activate() {
	a.win = gtk.NewApplicationWindow(a.gtkApp)
	a.win.SetTitle("HyprDynamicMonitors")
	a.win.SetDefaultSize(1000, 640)

	a.buildHeaderBar()

	// Left: draggable monitor canvas. Right: per-monitor settings.
	a.canvas = NewCanvas(a)
	a.sidebar = NewSidebar(a)

	paned := gtk.NewPaned(gtk.OrientationHorizontal)
	paned.SetWideHandle(true)
	paned.SetStartChild(a.canvas.Widget())
	paned.SetEndChild(a.sidebar.Widget())
	paned.SetResizeStartChild(true)
	paned.SetResizeEndChild(false)
	paned.SetPosition(600)

	// Vertical container: paned area on top, status bar at the bottom.
	root := gtk.NewBox(gtk.OrientationVertical, 0)
	root.SetVExpand(true)
	root.SetHExpand(true)
	paned.SetVExpand(true)
	root.Append(paned)
	root.Append(a.buildStatusBar())

	a.win.SetChild(root)
	a.refresh()
	a.win.SetVisible(true)
}

// buildHeaderBar creates the top bar: profile controls on the left, the live
// "Apply" action on the right.
func (a *App) buildHeaderBar() {
	header := gtk.NewHeaderBar()

	a.profileBar = NewProfileBar(a)
	for _, w := range a.profileBar.HeaderWidgets() {
		header.PackStart(w)
	}

	a.applyButton = gtk.NewButtonWithLabel("Apply")
	a.applyButton.AddCSSClass("suggested-action")
	a.applyButton.SetTooltipText("Apply the current layout to Hyprland live (hyprctl)")
	a.applyButton.ConnectClicked(a.onApplyClicked)
	header.PackEnd(a.applyButton)

	a.win.SetTitlebar(header)
}

func (a *App) buildStatusBar() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationHorizontal, 8)
	box.SetMarginStart(8)
	box.SetMarginEnd(8)
	box.SetMarginTop(4)
	box.SetMarginBottom(4)

	a.statusLabel = gtk.NewLabel("")
	a.statusLabel.SetXAlign(0)
	a.statusLabel.SetHExpand(true)
	a.statusLabel.SetEllipsize(3) // PANGO_ELLIPSIZE_END
	box.Append(a.statusLabel)

	return box
}

// refresh redraws the canvas and rebuilds the side panel from the current model.
// Call after any change to a.monitors / a.selected.
func (a *App) refresh() {
	if a.canvas != nil {
		a.canvas.Redraw()
	}
	if a.sidebar != nil {
		a.sidebar.Rebuild()
	}
}

// selectMonitor sets the active monitor by index and refreshes the UI.
func (a *App) selectMonitor(idx int) {
	if idx < 0 || idx >= len(a.monitors) {
		return
	}
	a.selected = idx
	a.refresh()
}

// selectedMonitor returns the currently selected monitor or nil.
func (a *App) selectedMonitor() *tui.MonitorSpec {
	if a.selected < 0 || a.selected >= len(a.monitors) {
		return nil
	}
	return a.monitors[a.selected]
}

// setStatus shows a transient message in the bottom status bar.
func (a *App) setStatus(format string, args ...any) {
	if a.statusLabel == nil {
		return
	}
	a.statusLabel.SetText(fmt.Sprintf(format, args...))
}

// setError shows an error message styled in red.
func (a *App) setError(err error) {
	if a.statusLabel == nil || err == nil {
		return
	}
	a.statusLabel.SetMarkup(fmt.Sprintf(`<span foreground="#cc0000">%s</span>`, gtkEscape(err.Error())))
}
