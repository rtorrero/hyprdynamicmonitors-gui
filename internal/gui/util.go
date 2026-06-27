package gui

import "html"

// gtkEscape escapes a string for safe use inside Pango/GTK markup.
func gtkEscape(s string) string {
	return html.EscapeString(s)
}
