package gui

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/fiffeek/hyprdynamicmonitors/internal/config"
	"github.com/fiffeek/hyprdynamicmonitors/internal/tui"
)

var profileNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

// ProfileBar holds the header-bar widgets for hyprdynamicmonitors profile
// management: a dropdown listing configured profiles plus a "Save as profile"
// action that freezes the current layout into a new profile.
type ProfileBar struct {
	app *App

	dropdown *gtk.DropDown
	editBtn  *gtk.Button
	saveBtn  *gtk.Button
	names    []string // dropdown index -> profile name
}

// NewProfileBar builds the profile widgets.
func NewProfileBar(app *App) *ProfileBar {
	p := &ProfileBar{app: app}

	p.dropdown = gtk.NewDropDownFromStrings(p.profileNames())
	p.dropdown.SetTooltipText("Profiles defined in your hyprdynamicmonitors config.\n" +
		"Select one to see the monitors/power/lid conditions that activate it.")
	p.dropdown.Connect("notify::selected", func() {
		p.showSelected(int(p.dropdown.Selected()))
	})

	p.editBtn = gtk.NewButtonFromIconName("view-filter-symbolic")
	p.editBtn.SetTooltipText("Edit the match conditions of the selected profile")
	p.editBtn.ConnectClicked(func() {
		idx := int(p.dropdown.Selected())
		if idx >= 0 && idx < len(p.names) {
			p.openConditionsEditor(p.names[idx])
		}
	})

	p.saveBtn = gtk.NewButtonFromIconName("document-save-symbolic")
	p.saveBtn.SetTooltipText("Save the current layout as a new profile")
	p.saveBtn.ConnectClicked(p.onSave)

	// Profile features require a config to know where to write profile files.
	if app.cfg == nil {
		p.saveBtn.SetSensitive(false)
		p.editBtn.SetSensitive(false)
		p.dropdown.SetSensitive(false)
	}

	return p
}

// HeaderWidgets returns the widgets to pack into the header bar.
func (p *ProfileBar) HeaderWidgets() []gtk.Widgetter {
	return []gtk.Widgetter{p.dropdown, p.editBtn, p.saveBtn}
}

// profileNames returns the configured profile names, ordered by their position
// in the config file. The result is cached in p.names so dropdown indices can be
// mapped back to profile names.
func (p *ProfileBar) profileNames() []string {
	p.names = nil
	if p.app.cfg == nil {
		return []string{"(no config)"}
	}
	raw := p.app.cfg.Get()
	if len(raw.Profiles) == 0 {
		return []string{"(no profiles)"}
	}
	for name := range raw.Profiles {
		p.names = append(p.names, name)
	}
	// Profile.KeyOrder reflects the order of "profiles.<name>" in the TOML.
	sort.Slice(p.names, func(i, j int) bool {
		return raw.Profiles[p.names[i]].KeyOrder < raw.Profiles[p.names[j]].KeyOrder
	})
	return p.names
}

// showSelected describes the chosen profile's activation conditions.
func (p *ProfileBar) showSelected(idx int) {
	if idx < 0 || idx >= len(p.names) {
		return
	}
	prof := p.app.cfg.Get().Profiles[p.names[idx]]
	if prof == nil {
		return
	}
	p.app.setStatus("Profile %q: %s", p.names[idx], conditionsSummary(prof))
}

// conditionsSummary renders a profile's conditions into a one-line description.
func conditionsSummary(prof *config.Profile) string {
	if prof.Conditions == nil {
		return "no conditions (fallback)"
	}
	var parts []string
	for _, rm := range prof.Conditions.RequiredMonitors {
		switch {
		case rm.Description != nil && *rm.Description != "":
			parts = append(parts, "desc:"+*rm.Description)
		case rm.Name != nil && *rm.Name != "":
			parts = append(parts, *rm.Name)
		}
	}
	monitors := "any monitors"
	if len(parts) > 0 {
		monitors = fmt.Sprintf("%d monitor(s): %s", len(parts), strings.Join(parts, ", "))
	}
	if prof.Conditions.PowerState != nil {
		monitors += " · power=" + prof.Conditions.PowerState.Value()
	}
	if prof.Conditions.LidState != nil {
		monitors += " · lid=" + prof.Conditions.LidState.Value()
	}
	return monitors
}

// refresh repopulates the dropdown after a config change.
func (p *ProfileBar) refresh() {
	p.dropdown.SetModel(gtk.NewStringList(p.profileNames()))
}

// onSave prompts for a profile name and freezes the current layout into it.
func (p *ProfileBar) onSave() {
	dialog := gtk.NewWindow()
	dialog.SetTransientFor(&p.app.win.Window)
	dialog.SetModal(true)
	dialog.SetTitle("Save as profile")
	dialog.SetDefaultSize(360, -1)

	box := gtk.NewBox(gtk.OrientationVertical, 12)
	box.SetMarginStart(16)
	box.SetMarginEnd(16)
	box.SetMarginTop(16)
	box.SetMarginBottom(16)

	box.Append(gtk.NewLabel("New profile name:"))

	entry := gtk.NewEntry()
	entry.SetPlaceholderText("e.g. docked_dual")
	box.Append(entry)

	errLabel := gtk.NewLabel("")
	errLabel.SetXAlign(0)
	errLabel.AddCSSClass("error")
	box.Append(errLabel)

	btnRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	btnRow.SetHAlign(gtk.AlignEnd)
	cancel := gtk.NewButtonWithLabel("Cancel")
	save := gtk.NewButtonWithLabel("Save")
	save.AddCSSClass("suggested-action")
	btnRow.Append(cancel)
	btnRow.Append(save)
	box.Append(btnRow)

	cancel.ConnectClicked(func() { dialog.Close() })
	save.ConnectClicked(func() {
		name := entry.Text()
		if err := validateProfileName(name); err != nil {
			errLabel.SetText(err.Error())
			return
		}
		if err := p.freeze(name); err != nil {
			errLabel.SetText(err.Error())
			return
		}
		dialog.Close()
	})
	entry.ConnectActivate(func() { save.Activate() })

	dialog.SetChild(box)
	dialog.SetVisible(true)
}

// freeze converts the current layout and writes a new profile via the same
// profilemaker service the TUI uses.
func (p *ProfileBar) freeze(name string) error {
	hyprMonitors, err := tui.ConvertToHyprMonitors(p.app.monitors)
	if err != nil {
		return fmt.Errorf("invalid layout: %w", err)
	}

	// Mirror the TUI's convention for the profile template location.
	file := fmt.Sprintf("hyprconfigs/%s.go.tmpl", name)
	if err := p.app.profileMaker.FreezeGivenAs(name, file, hyprMonitors); err != nil {
		return fmt.Errorf("cant create profile: %w", err)
	}

	// Reload config so the new profile shows up in the dropdown.
	if err := p.app.cfg.Reload(); err != nil {
		return fmt.Errorf("profile saved but reload failed: %w", err)
	}
	p.refresh()
	p.app.setStatus("Saved profile %q -> %s", name, file)
	return nil
}

// validateProfileName mirrors the TUI's profile-name rules.
func validateProfileName(name string) error {
	switch {
	case name == "":
		return errors.New("profile name cannot be empty")
	case len(name) > 50:
		return errors.New("profile name must be 50 characters or less")
	case !profileNameRe.MatchString(name):
		return errors.New("must start with a letter; letters, numbers, '-' and '_' only")
	}
	return nil
}
