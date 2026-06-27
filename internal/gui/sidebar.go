package gui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/fiffeek/hyprdynamicmonitors/internal/tui"
)

// Sidebar is the right-hand per-monitor settings panel.
type Sidebar struct {
	app *App

	scroller *gtk.ScrolledWindow
	content  *gtk.Box // rebuilt on every selection change

	// References needed for live sync during canvas drags.
	posX *gtk.SpinButton
	posY *gtk.SpinButton

	// Guards programmatic widget updates from triggering change handlers.
	building bool
}

// NewSidebar creates the (initially empty) settings panel.
func NewSidebar(app *App) *Sidebar {
	s := &Sidebar{app: app}
	s.scroller = gtk.NewScrolledWindow()
	s.scroller.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	s.scroller.SetSizeRequest(340, -1)
	return s
}

// Widget returns the root widget for embedding in the layout.
func (s *Sidebar) Widget() gtk.Widgetter {
	return s.scroller
}

// Rebuild regenerates the entire form for the currently selected monitor.
func (s *Sidebar) Rebuild() {
	s.building = true
	defer func() { s.building = false }()

	box := gtk.NewBox(gtk.OrientationVertical, 10)
	box.SetMarginStart(12)
	box.SetMarginEnd(12)
	box.SetMarginTop(12)
	box.SetMarginBottom(12)

	box.Append(s.buildMonitorSelector())

	m := s.app.selectedMonitor()
	if m == nil {
		lbl := gtk.NewLabel("No monitor selected")
		lbl.SetXAlign(0)
		box.Append(lbl)
	} else {
		box.Append(gtk.NewSeparator(gtk.OrientationHorizontal))
		box.Append(s.buildForm(m))
	}

	s.content = box
	s.scroller.SetChild(box)
}

// SyncPosition refreshes only the X/Y spin buttons (used while dragging).
func (s *Sidebar) SyncPosition() {
	m := s.app.selectedMonitor()
	if m == nil || s.posX == nil || s.posY == nil {
		return
	}
	s.building = true
	s.posX.SetValue(float64(m.X))
	s.posY.SetValue(float64(m.Y))
	s.building = false
}

// buildMonitorSelector is a dropdown to switch the active monitor (mirrors
// clicking on the canvas).
func (s *Sidebar) buildMonitorSelector() *gtk.Box {
	row := s.labeledRow("Monitor")

	names := make([]string, 0, len(s.app.monitors))
	for _, m := range s.app.monitors {
		names = append(names, fmt.Sprintf("%s — %s", m.Name, m.Description))
	}
	dd := gtk.NewDropDownFromStrings(names)
	if s.app.selected >= 0 {
		dd.SetSelected(uint(s.app.selected))
	}
	dd.SetHExpand(true)
	dd.Connect("notify::selected", func() {
		if s.building {
			return
		}
		s.app.selectMonitor(int(dd.Selected()))
	})
	row.Append(dd)
	return row
}

// buildForm builds the editable controls for a single monitor.
func (s *Sidebar) buildForm(m *tui.MonitorSpec) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 10)

	// Description (read-only).
	if m.Description != "" {
		desc := gtk.NewLabel(m.Description)
		desc.SetXAlign(0)
		desc.SetWrap(true)
		desc.AddCSSClass("dim-label")
		box.Append(desc)
	}

	// Enabled switch.
	enabledRow := s.labeledRow("Enabled")
	enabled := gtk.NewSwitch()
	enabled.SetActive(!m.Disabled)
	enabled.SetHAlign(gtk.AlignStart)
	enabled.ConnectStateSet(func(state bool) bool {
		if s.building {
			return false
		}
		m.Disabled = !state
		s.app.canvas.Redraw()
		s.app.setStatus("%s %s", m.Name, map[bool]string{true: "enabled", false: "disabled"}[state])
		return false
	})
	enabledRow.Append(enabled)
	box.Append(enabledRow)

	// Resolution + refresh.
	box.Append(s.buildModeControls(m))

	// Scale.
	scaleRow := s.labeledRow("Scale")
	scale := gtk.NewSpinButtonWithRange(0.1, 10.0, 0.05)
	scale.SetDigits(4)
	scale.SetValue(m.Scale)
	scale.SetHExpand(true)
	scale.ConnectValueChanged(func() {
		if s.building {
			return
		}
		valid, err := m.ClosestValidScale(scale.Value(), true, true)
		if err != nil {
			s.app.setError(err)
			return
		}
		m.Scale = valid
		s.app.canvas.Redraw()
		s.app.setStatus("%s scale %.4f (%s)", m.Name, m.Scale, m.ScalePretty(true))
	})
	scaleRow.Append(scale)
	box.Append(scaleRow)

	// Position X/Y.
	posRow := s.labeledRow("Position")
	posBox := gtk.NewBox(gtk.OrientationHorizontal, 6)
	s.posX = gtk.NewSpinButtonWithRange(-16384, 16384, 1)
	s.posX.SetValue(float64(m.X))
	s.posX.ConnectValueChanged(func() {
		if s.building {
			return
		}
		m.X = int(s.posX.Value())
		s.app.canvas.Redraw()
	})
	s.posY = gtk.NewSpinButtonWithRange(-16384, 16384, 1)
	s.posY.SetValue(float64(m.Y))
	s.posY.ConnectValueChanged(func() {
		if s.building {
			return
		}
		m.Y = int(s.posY.Value())
		s.app.canvas.Redraw()
	})
	posBox.Append(gtk.NewLabel("X"))
	posBox.Append(s.posX)
	posBox.Append(gtk.NewLabel("Y"))
	posBox.Append(s.posY)
	posRow.Append(posBox)
	box.Append(posRow)

	// Rotation.
	rotRow := s.labeledRow("Rotation")
	rotations := []string{"0°", "90°", "180°", "270°"}
	rot := gtk.NewDropDownFromStrings(rotations)
	rot.SetSelected(uint(m.Transform))
	rot.SetHExpand(true)
	rot.Connect("notify::selected", func() {
		if s.building {
			return
		}
		m.Transform = int(rot.Selected())
		s.app.canvas.Redraw()
		s.app.setStatus("%s %s", m.Name, m.RotationPretty(true))
	})
	rotRow.Append(rot)
	box.Append(rotRow)

	// Flip + VRR toggles.
	togglesRow := gtk.NewBox(gtk.OrientationHorizontal, 16)
	flip := gtk.NewCheckButtonWithLabel("Flipped")
	flip.SetActive(m.Flipped)
	flip.ConnectToggled(func() {
		if s.building {
			return
		}
		m.Flipped = flip.Active()
	})
	vrr := gtk.NewCheckButtonWithLabel("VRR")
	vrr.SetActive(m.Vrr)
	vrr.ConnectToggled(func() {
		if s.building {
			return
		}
		m.Vrr = vrr.Active()
	})
	togglesRow.Append(flip)
	togglesRow.Append(vrr)
	box.Append(togglesRow)

	return box
}

// buildModeControls builds the resolution dropdown and a dependent refresh-rate
// dropdown, both driven by the monitor's reported available modes.
func (s *Sidebar) buildModeControls(m *tui.MonitorSpec) *gtk.Box {
	wrap := gtk.NewBox(gtk.OrientationVertical, 10)

	resolutions, byRes := groupModes(m.AvailableModes)
	currentRes := fmt.Sprintf("%dx%d", m.Width, m.Height)

	resRow := s.labeledRow("Resolution")
	resDD := gtk.NewDropDownFromStrings(resolutions)
	resDD.SetHExpand(true)
	resRow.Append(resDD)

	refreshRow := s.labeledRow("Refresh")
	// Placeholder dropdown; populated by syncRefresh.
	refreshDD := gtk.NewDropDownFromStrings([]string{})
	refreshDD.SetHExpand(true)
	refreshRow.Append(refreshDD)

	// populateRefresh refills the refresh dropdown for the chosen resolution and
	// selects the rate matching the monitor's current mode when possible.
	populateRefresh := func(res string) {
		modes := byRes[res]
		labels := make([]string, 0, len(modes))
		for _, full := range modes {
			labels = append(labels, refreshLabel(full))
		}
		s.building = true
		refreshDD.SetModel(gtk.NewStringList(labels))
		// Try to keep the current refresh rate selected.
		want := fmt.Sprintf("%.2f", m.RefreshRate)
		sel := uint(0)
		for i, full := range modes {
			if strings.HasPrefix(refreshLabel(full), want) {
				sel = uint(i)
			}
		}
		refreshDD.SetSelected(sel)
		s.building = false
	}

	applyMode := func() {
		res := resolutions[resDD.Selected()]
		modes := byRes[res]
		if len(modes) == 0 {
			return
		}
		idx := int(refreshDD.Selected())
		if idx < 0 || idx >= len(modes) {
			idx = 0
		}
		if err := m.SetMode(modes[idx]); err != nil {
			s.app.setError(err)
			return
		}
		s.app.canvas.Redraw()
		s.app.setStatus("%s mode %s", m.Name, m.ModeForComparison())
	}

	// Select the current resolution.
	for i, r := range resolutions {
		if r == currentRes {
			resDD.SetSelected(uint(i))
		}
	}
	populateRefresh(currentRes)

	resDD.Connect("notify::selected", func() {
		if s.building {
			return
		}
		populateRefresh(resolutions[resDD.Selected()])
		applyMode()
	})
	refreshDD.Connect("notify::selected", func() {
		if s.building {
			return
		}
		applyMode()
	})

	wrap.Append(resRow)
	wrap.Append(refreshRow)
	return wrap
}

// labeledRow returns a horizontal box with a fixed-width label.
func (s *Sidebar) labeledRow(label string) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontal, 10)
	lbl := gtk.NewLabel(label)
	lbl.SetXAlign(0)
	lbl.SetSizeRequest(80, -1)
	row.Append(lbl)
	return row
}

// groupModes splits available mode strings ("WxH@RR.RRHz") into an ordered list
// of unique resolutions and a map resolution -> full mode strings.
func groupModes(modes []string) ([]string, map[string][]string) {
	var order []string
	byRes := map[string][]string{}
	for _, full := range modes {
		at := strings.Index(full, "@")
		if at < 0 {
			continue
		}
		res := full[:at]
		if _, ok := byRes[res]; !ok {
			order = append(order, res)
		}
		byRes[res] = append(byRes[res], full)
	}
	return order, byRes
}

// refreshLabel extracts the human refresh portion ("120.00Hz") from a mode string.
func refreshLabel(full string) string {
	at := strings.Index(full, "@")
	if at < 0 {
		return full
	}
	return full[at+1:]
}
