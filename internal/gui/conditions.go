package gui

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/fiffeek/hyprdynamicmonitors/internal/config"
	"github.com/fiffeek/hyprdynamicmonitors/internal/utils"
)

// Match-by dropdown indices.
const (
	matchByDescription = 0
	matchByName        = 1
)

// condRow holds the widgets of a single required-monitor row in the editor.
type condRow struct {
	box     *gtk.Box
	matchDD *gtk.DropDown // 0 = Description, 1 = Name
	value   *gtk.Entry
	regex   *gtk.CheckButton
	tag     *gtk.Entry
}

// conditionsEditor is the modal dialog for editing one profile's match conditions.
type conditionsEditor struct {
	bar  *ProfileBar
	name string

	dialog   *gtk.Window
	rowsBox  *gtk.Box
	rows     []*condRow
	powerDD  *gtk.DropDown
	lidDD    *gtk.DropDown
	errLabel *gtk.Label
}

// openConditionsEditor pops up the editor for the named profile.
func (p *ProfileBar) openConditionsEditor(name string) {
	if p.app.cfg == nil {
		return
	}
	prof := p.app.cfg.Get().Profiles[name]
	if prof == nil {
		p.app.setError(fmt.Errorf("profile %q not found", name))
		return
	}

	e := &conditionsEditor{bar: p, name: name}
	e.build(prof)
}

func (e *conditionsEditor) build(prof *config.Profile) {
	e.dialog = gtk.NewWindow()
	e.dialog.SetTransientFor(&e.bar.app.win.Window)
	e.dialog.SetModal(true)
	e.dialog.SetTitle("Edit conditions: " + e.name)
	e.dialog.SetDefaultSize(520, 480)

	outer := gtk.NewBox(gtk.OrientationVertical, 12)
	outer.SetMarginStart(16)
	outer.SetMarginEnd(16)
	outer.SetMarginTop(16)
	outer.SetMarginBottom(16)

	intro := gtk.NewLabel("The daemon auto-selects this profile when the connected " +
		"monitors (and optional power/lid state) match these conditions.")
	intro.SetWrap(true)
	intro.SetXAlign(0)
	intro.AddCSSClass("dim-label")
	outer.Append(intro)

	// Required monitors.
	header := gtk.NewBox(gtk.OrientationHorizontal, 8)
	title := gtk.NewLabel("Required monitors")
	title.SetXAlign(0)
	title.SetHExpand(true)
	title.AddCSSClass("heading")
	addBtn := gtk.NewButtonFromIconName("list-add-symbolic")
	addBtn.SetTooltipText("Add a required monitor")
	addBtn.ConnectClicked(func() { e.addRow(nil) })
	header.Append(title)
	header.Append(addBtn)
	outer.Append(header)

	rowsScroll := gtk.NewScrolledWindow()
	rowsScroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	rowsScroll.SetVExpand(true)
	e.rowsBox = gtk.NewBox(gtk.OrientationVertical, 8)
	rowsScroll.SetChild(e.rowsBox)
	outer.Append(rowsScroll)

	if prof.Conditions != nil {
		for _, rm := range prof.Conditions.RequiredMonitors {
			e.addRow(rm)
		}
	}
	if len(e.rows) == 0 {
		e.addRow(nil)
	}

	// Power + lid state.
	stateGrid := gtk.NewBox(gtk.OrientationHorizontal, 16)

	powerBox := gtk.NewBox(gtk.OrientationVertical, 4)
	powerBox.Append(leftLabel("Power state"))
	e.powerDD = gtk.NewDropDownFromStrings([]string{"Any", "AC", "BAT"})
	e.powerDD.SetSelected(powerToIndex(prof))
	powerBox.Append(e.powerDD)
	stateGrid.Append(powerBox)

	lidBox := gtk.NewBox(gtk.OrientationVertical, 4)
	lidBox.Append(leftLabel("Lid state"))
	e.lidDD = gtk.NewDropDownFromStrings([]string{"Any", "Opened", "Closed"})
	e.lidDD.SetSelected(lidToIndex(prof))
	lidBox.Append(e.lidDD)
	stateGrid.Append(lidBox)

	outer.Append(stateGrid)

	// Error + buttons.
	e.errLabel = gtk.NewLabel("")
	e.errLabel.SetXAlign(0)
	e.errLabel.SetWrap(true)
	e.errLabel.AddCSSClass("error")
	outer.Append(e.errLabel)

	btnRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	btnRow.SetHAlign(gtk.AlignEnd)
	cancel := gtk.NewButtonWithLabel("Cancel")
	cancel.ConnectClicked(func() { e.dialog.Close() })
	save := gtk.NewButtonWithLabel("Save")
	save.AddCSSClass("suggested-action")
	save.ConnectClicked(e.onSave)
	btnRow.Append(cancel)
	btnRow.Append(save)
	outer.Append(btnRow)

	e.dialog.SetChild(outer)
	e.dialog.SetVisible(true)
}

// addRow appends an editable required-monitor row, optionally prefilled.
func (e *conditionsEditor) addRow(rm *config.RequiredMonitor) {
	row := &condRow{}
	row.box = gtk.NewBox(gtk.OrientationHorizontal, 6)

	row.matchDD = gtk.NewDropDownFromStrings([]string{"Description", "Name"})
	row.value = gtk.NewEntry()
	row.value.SetHExpand(true)
	row.value.SetPlaceholderText("match value")
	row.regex = gtk.NewCheckButtonWithLabel("regex")
	row.tag = gtk.NewEntry()
	row.tag.SetPlaceholderText("tag (optional)")
	row.tag.SetMaxWidthChars(12)

	if rm != nil {
		switch {
		case rm.Description != nil:
			row.matchDD.SetSelected(matchByDescription)
			row.value.SetText(*rm.Description)
			row.regex.SetActive(rm.MatchDescriptionUsingRegex != nil && *rm.MatchDescriptionUsingRegex)
		case rm.Name != nil:
			row.matchDD.SetSelected(matchByName)
			row.value.SetText(*rm.Name)
			row.regex.SetActive(rm.MatchNameUsingRegex != nil && *rm.MatchNameUsingRegex)
		}
		if rm.MonitorTag != nil {
			row.tag.SetText(*rm.MonitorTag)
		}
	}

	remove := gtk.NewButtonFromIconName("list-remove-symbolic")
	remove.SetTooltipText("Remove this monitor")
	remove.ConnectClicked(func() {
		e.rowsBox.Remove(row.box)
		for i, r := range e.rows {
			if r == row {
				e.rows = append(e.rows[:i], e.rows[i+1:]...)
				break
			}
		}
	})

	row.box.Append(row.matchDD)
	row.box.Append(row.value)
	row.box.Append(row.regex)
	row.box.Append(row.tag)
	row.box.Append(remove)

	e.rowsBox.Append(row.box)
	e.rows = append(e.rows, row)
}

// onSave gathers the form into a ProfileCondition and writes it back to the TOML.
func (e *conditionsEditor) onSave() {
	cond := &config.ProfileCondition{}

	for _, row := range e.rows {
		val := strings.TrimSpace(row.value.Text())
		if val == "" {
			continue // skip empty rows
		}
		rm := &config.RequiredMonitor{}
		useRegex := row.regex.Active()
		if row.matchDD.Selected() == matchByName {
			rm.Name = utils.StringPtr(val)
			if useRegex {
				rm.MatchNameUsingRegex = utils.BoolPtr(true)
			}
		} else {
			rm.Description = utils.StringPtr(val)
			if useRegex {
				rm.MatchDescriptionUsingRegex = utils.BoolPtr(true)
			}
		}
		if tag := strings.TrimSpace(row.tag.Text()); tag != "" {
			rm.MonitorTag = utils.StringPtr(tag)
		}
		cond.RequiredMonitors = append(cond.RequiredMonitors, rm)
	}

	if len(cond.RequiredMonitors) == 0 {
		e.errLabel.SetText("at least one required monitor is needed")
		return
	}

	switch e.powerDD.Selected() {
	case 1:
		cond.PowerState = utils.JustPtr(config.AC)
	case 2:
		cond.PowerState = utils.JustPtr(config.BAT)
	}
	switch e.lidDD.Selected() {
	case 1:
		cond.LidState = utils.JustPtr(config.OpenedLidStateType)
	case 2:
		cond.LidState = utils.JustPtr(config.ClosedLidStateType)
	}

	if err := e.bar.saveConditions(e.name, cond); err != nil {
		e.errLabel.SetText(err.Error())
		return
	}
	e.dialog.Close()
}

// saveConditions rebuilds the profile's TOML block in place, preserving the rest
// of the config file (comments, formatting, other sections).
func (p *ProfileBar) saveConditions(name string, cond *config.ProfileCondition) error {
	raw := p.app.cfg.Get()
	prof := raw.Profiles[name]
	if prof == nil {
		return fmt.Errorf("profile %q not found", name)
	}

	content, err := os.ReadFile(raw.ConfigPath)
	if err != nil {
		return fmt.Errorf("cant read config: %w", err)
	}

	updated, err := rewriteProfileConditions(string(content), prof, name, cond)
	if err != nil {
		return err
	}

	if err := utils.WriteAtomic(raw.ConfigPath, []byte(updated)); err != nil {
		return fmt.Errorf("cant write config: %w", err)
	}

	if err := p.app.cfg.Reload(); err != nil {
		return fmt.Errorf("saved but reload failed: %w", err)
	}
	p.refresh()
	p.app.setStatus("Updated conditions for %q: %s", name, conditionsSummary(p.app.cfg.Get().Profiles[name]))
	return nil
}

// rewriteProfileConditions returns `content` with the named profile's TOML block
// replaced by a freshly-encoded one carrying `cond`, preserving the rest of the
// file (comments, formatting, other sections) and the original config_file path.
func rewriteProfileConditions(content string, prof *config.Profile, name string, cond *config.ProfileCondition) (string, error) {
	lines := strings.Split(content, "\n")

	start, end := profileBlockRange(lines, name)
	if start < 0 {
		return "", fmt.Errorf("cant locate [profiles.%s] block in config", name)
	}

	// Preserve the original config_file string exactly as written (Load mutates
	// the in-memory value into an absolute path).
	origConfigFile := scrapeField(lines[start:end], "config_file")

	edited := *prof
	edited.Name = name
	edited.Conditions = cond
	if origConfigFile != "" {
		edited.ConfigFile = origConfigFile
	}

	block, err := encodeProfileBlock(&edited)
	if err != nil {
		return "", err
	}

	var out []string
	out = append(out, lines[:start]...)
	out = append(out, strings.Split(block, "\n")...)
	out = append(out, "")
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n"), nil
}

// encodeProfileBlock serializes a single profile to its `[profiles.<name>]...`
// TOML block (without the bare `[profiles]` header).
func encodeProfileBlock(prof *config.Profile) (string, error) {
	wrapper := config.RawConfig{
		Profiles: map[string]*config.Profile{prof.Name: prof},
	}
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(wrapper); err != nil {
		return "", fmt.Errorf("cant encode profile: %w", err)
	}
	s := strings.Replace(buf.String(), "[profiles]\n", "", 1)
	return strings.TrimSpace(s), nil
}

// profileBlockRange finds the [start,end) line range covering all sections that
// belong to the named profile (`[profiles.<name>]` and any children). end points
// at the first foreign section header (or len(lines)).
func profileBlockRange(lines []string, name string) (int, int) {
	prefix := "profiles." + name
	start := -1
	for i, line := range lines {
		path, ok := sectionPath(line)
		if !ok {
			continue
		}
		belongs := path == prefix || strings.HasPrefix(path, prefix+".")
		if start < 0 {
			if belongs {
				start = i
			}
			continue
		}
		if !belongs {
			return start, trimTrailingBlanks(lines, start, i)
		}
	}
	if start < 0 {
		return -1, -1
	}
	return start, trimTrailingBlanks(lines, start, len(lines))
}

// trimTrailingBlanks moves `end` back over blank lines so they stay attached to
// whatever follows rather than being swallowed into the replaced block.
func trimTrailingBlanks(lines []string, start, end int) int {
	for end > start+1 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return end
}

// sectionPath returns the dotted path of a TOML section header line, e.g.
// "[[profiles.X.conditions.required_monitors]]" -> "profiles.X.conditions.required_monitors".
func sectionPath(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "[") {
		return "", false
	}
	t = strings.TrimPrefix(t, "[")
	t = strings.TrimPrefix(t, "[")
	t = strings.TrimSuffix(t, "]")
	t = strings.TrimSuffix(t, "]")
	return strings.TrimSpace(t), true
}

// scrapeField extracts a top-level `key = "value"` string value from a block.
func scrapeField(block []string, key string) string {
	for _, line := range block {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, key) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(t, key))
		if !strings.HasPrefix(rest, "=") {
			continue
		}
		valStr := strings.TrimSpace(strings.TrimPrefix(rest, "="))
		if unquoted, err := strconv.Unquote(valStr); err == nil {
			return unquoted
		}
		return valStr
	}
	return ""
}

// leftLabel is a small left-aligned label helper.
func leftLabel(text string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.SetXAlign(0)
	return l
}

// powerToIndex / lidToIndex map a profile's state condition to a dropdown index.
func powerToIndex(prof *config.Profile) uint {
	if prof.Conditions == nil || prof.Conditions.PowerState == nil {
		return 0
	}
	if *prof.Conditions.PowerState == config.AC {
		return 1
	}
	return 2
}

func lidToIndex(prof *config.Profile) uint {
	if prof.Conditions == nil || prof.Conditions.LidState == nil {
		return 0
	}
	if *prof.Conditions.LidState == config.OpenedLidStateType {
		return 1
	}
	return 2
}
