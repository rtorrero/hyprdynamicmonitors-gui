package gui

import (
	"math"

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/fiffeek/hyprdynamicmonitors/internal/tui"
)

// snapPixels is the on-screen distance (in pixels) within which monitor edges
// snap to one another, mirroring wdisplays' SNAP_DIST behaviour.
const snapPixels = 8.0

// Canvas renders monitors as draggable rectangles in a virtual layout, à la
// wdisplays. The geometry uses logical coordinates (Hyprland layout space) and
// is mapped to widget pixels via `scale` + `offset`.
type Canvas struct {
	app  *App
	area *gtk.DrawingArea

	scale   float64 // pixels per logical unit; 0 means "auto-fit on next draw"
	offsetX float64 // pixel offset of logical origin
	offsetY float64

	// Drag state.
	dragIdx     int     // monitor being dragged, -1 == panning
	dragStartX  float64 // gesture start (widget px)
	dragStartY  float64
	origMonX    int // monitor position when the drag started
	origMonY    int
	origOffsetX float64 // pan offset when the drag started
	origOffsetY float64
	moved       bool
}

// NewCanvas builds the drawing area and installs the interaction controllers.
func NewCanvas(app *App) *Canvas {
	c := &Canvas{app: app, dragIdx: -1}

	c.area = gtk.NewDrawingArea()
	c.area.SetHExpand(true)
	c.area.SetVExpand(true)
	c.area.SetSizeRequest(400, 300)
	c.area.SetDrawFunc(c.draw)

	// Left-button drag: move a monitor, or pan if started on empty space.
	drag := gtk.NewGestureDrag()
	drag.SetButton(1)
	drag.ConnectDragBegin(c.onDragBegin)
	drag.ConnectDragUpdate(c.onDragUpdate)
	drag.ConnectDragEnd(c.onDragEnd)
	c.area.AddController(drag)

	// Ctrl+scroll to zoom.
	scroll := gtk.NewEventControllerScroll(gtk.EventControllerScrollVertical)
	scroll.ConnectScroll(func(_, dy float64) bool {
		factor := 1.0 - 0.1*dy
		c.zoom(factor)
		return true
	})
	c.area.AddController(scroll)

	return c
}

// Widget returns the root widget for embedding in the layout.
func (c *Canvas) Widget() gtk.Widgetter {
	frame := gtk.NewFrame("")
	frame.SetChild(c.area)
	frame.SetMarginStart(6)
	frame.SetMarginEnd(6)
	frame.SetMarginTop(6)
	frame.SetMarginBottom(6)
	return frame
}

// Redraw queues a repaint.
func (c *Canvas) Redraw() {
	if c.area != nil {
		c.area.QueueDraw()
	}
}

// monitorRect returns the monitor's visual rectangle in logical coordinates,
// accounting for rotation (90/270 swap width/height) and scale.
func monitorRect(m *tui.MonitorSpec) (x, y, w, h float64) {
	sw := float64(m.Width) / m.Scale
	sh := float64(m.Height) / m.Scale
	if m.NeedsDimensionsSwap() {
		sw, sh = sh, sw
	}
	return float64(m.X), float64(m.Y), sw, sh
}

// bounds computes the logical bounding box across all monitors.
func (c *Canvas) bounds() (minX, minY, maxX, maxY float64) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, m := range c.app.monitors {
		x, y, w, h := monitorRect(m)
		minX = math.Min(minX, x)
		minY = math.Min(minY, y)
		maxX = math.Max(maxX, x+w)
		maxY = math.Max(maxY, y+h)
	}
	if math.IsInf(minX, 1) { // no monitors
		return 0, 0, 1920, 1080
	}
	return minX, minY, maxX, maxY
}

// autoFit recomputes scale + offset so the whole layout fits the viewport.
func (c *Canvas) autoFit(width, height int) {
	minX, minY, maxX, maxY := c.bounds()
	const pad = 40.0
	bw := math.Max(maxX-minX, 1)
	bh := math.Max(maxY-minY, 1)
	sx := (float64(width) - 2*pad) / bw
	sy := (float64(height) - 2*pad) / bh
	c.scale = math.Max(0.01, math.Min(sx, sy))
	// Center the layout.
	c.offsetX = (float64(width)-bw*c.scale)/2 - minX*c.scale
	c.offsetY = (float64(height)-bh*c.scale)/2 - minY*c.scale
}

func (c *Canvas) zoom(factor float64) {
	if c.scale == 0 {
		return
	}
	w := float64(c.area.Width())
	h := float64(c.area.Height())
	cx, cy := w/2, h/2
	// Keep the viewport center fixed while zooming.
	lx := (cx - c.offsetX) / c.scale
	ly := (cy - c.offsetY) / c.scale
	c.scale = math.Max(0.01, math.Min(50, c.scale*factor))
	c.offsetX = cx - lx*c.scale
	c.offsetY = cy - ly*c.scale
	c.Redraw()
}

// logicalToPixel / pixelToLogical convert between coordinate spaces.
func (c *Canvas) toPixel(lx, ly float64) (float64, float64) {
	return lx*c.scale + c.offsetX, ly*c.scale + c.offsetY
}

func (c *Canvas) toLogical(px, py float64) (float64, float64) {
	return (px - c.offsetX) / c.scale, (py - c.offsetY) / c.scale
}

// hitTest returns the index of the topmost monitor under the widget point, or -1.
func (c *Canvas) hitTest(px, py float64) int {
	lx, ly := c.toLogical(px, py)
	for i := len(c.app.monitors) - 1; i >= 0; i-- {
		x, y, w, h := monitorRect(c.app.monitors[i])
		if lx >= x && lx <= x+w && ly >= y && ly <= y+h {
			return i
		}
	}
	return -1
}

// draw paints the whole canvas.
func (c *Canvas) draw(_ *gtk.DrawingArea, cr *cairo.Context, width, height int) {
	if c.scale == 0 {
		c.autoFit(width, height)
	}

	// Background.
	cr.SetSourceRGB(0.13, 0.14, 0.17)
	cr.Rectangle(0, 0, float64(width), float64(height))
	cr.Fill()

	for i, m := range c.app.monitors {
		c.drawMonitor(cr, i, m)
	}
}

func (c *Canvas) drawMonitor(cr *cairo.Context, idx int, m *tui.MonitorSpec) {
	x, y, w, h := monitorRect(m)
	px, py := c.toPixel(x, y)
	pw, ph := w*c.scale, h*c.scale

	selected := idx == c.app.selected

	// Fill.
	switch {
	case m.Disabled:
		cr.SetSourceRGBA(0.3, 0.3, 0.32, 0.7)
	case selected:
		cr.SetSourceRGBA(0.20, 0.45, 0.85, 0.85)
	default:
		cr.SetSourceRGBA(0.25, 0.28, 0.34, 0.9)
	}
	cr.Rectangle(px, py, pw, ph)
	cr.Fill()

	// Border.
	if selected {
		cr.SetSourceRGB(0.45, 0.7, 1.0)
		cr.SetLineWidth(3)
	} else {
		cr.SetSourceRGB(0.5, 0.52, 0.58)
		cr.SetLineWidth(1.5)
	}
	cr.Rectangle(px, py, pw, ph)
	cr.Stroke()

	// Label: name + mode.
	cr.SetSourceRGB(0.95, 0.96, 0.98)
	cr.SelectFontFace("sans-serif", cairo.FontSlantNormal, cairo.FontWeightBold)
	cr.SetFontSize(14)
	cr.MoveTo(px+8, py+20)
	cr.ShowText(m.Name)

	cr.SelectFontFace("sans-serif", cairo.FontSlantNormal, cairo.FontWeightNormal)
	cr.SetFontSize(11)
	cr.MoveTo(px+8, py+38)
	if m.Disabled {
		cr.ShowText("(disabled)")
	} else {
		cr.ShowText(m.ModePretty())
	}
}

// --- Drag handling -------------------------------------------------------

func (c *Canvas) onDragBegin(startX, startY float64) {
	c.dragStartX = startX
	c.dragStartY = startY
	c.moved = false
	c.origOffsetX = c.offsetX
	c.origOffsetY = c.offsetY

	c.dragIdx = c.hitTest(startX, startY)
	if c.dragIdx >= 0 {
		m := c.app.monitors[c.dragIdx]
		c.origMonX = m.X
		c.origMonY = m.Y
	}
}

func (c *Canvas) onDragUpdate(offsetX, offsetY float64) {
	if math.Abs(offsetX) > 2 || math.Abs(offsetY) > 2 {
		c.moved = true
	}

	if c.dragIdx < 0 {
		// Panning.
		c.offsetX = c.origOffsetX + offsetX
		c.offsetY = c.origOffsetY + offsetY
		c.Redraw()
		return
	}

	m := c.app.monitors[c.dragIdx]
	newX := c.origMonX + int(math.Round(offsetX/c.scale))
	newY := c.origMonY + int(math.Round(offsetY/c.scale))
	m.X, m.Y = c.snap(c.dragIdx, newX, newY)
	c.Redraw()
	c.app.sidebar.SyncPosition() // keep the spin buttons in sync while dragging
}

func (c *Canvas) onDragEnd(offsetX, offsetY float64) {
	if c.dragIdx >= 0 && !c.moved {
		// Treated as a plain click: select the monitor.
		c.app.selectMonitor(c.dragIdx)
	} else if c.dragIdx >= 0 {
		c.app.selectMonitor(c.dragIdx)
		c.app.setStatus("Moved %s to %d,%d", c.app.monitors[c.dragIdx].Name,
			c.app.monitors[c.dragIdx].X, c.app.monitors[c.dragIdx].Y)
	}
	c.dragIdx = -1
}

// snap aligns the dragged monitor's edges to nearby monitors' edges.
func (c *Canvas) snap(idx, x, y int) (int, int) {
	threshold := snapPixels / c.scale
	dragged := c.app.monitors[idx]
	_, _, dw, dh := monitorRect(dragged)

	bestDX, bestDY := math.Inf(1), math.Inf(1)
	snapX, snapY := x, y

	candidatesX := []float64{float64(x), float64(x) + dw} // left, right edges
	candidatesY := []float64{float64(y), float64(y) + dh} // top, bottom edges

	for i, other := range c.app.monitors {
		if i == idx {
			continue
		}
		ox, oy, ow, oh := monitorRect(other)
		targetsX := []float64{ox, ox + ow}
		targetsY := []float64{oy, oy + oh}

		for ci, ce := range candidatesX {
			for _, te := range targetsX {
				if d := math.Abs(ce - te); d < threshold && d < bestDX {
					bestDX = d
					snapX = int(te - dw*float64(ci)) // ci==0 left, ci==1 right
				}
			}
		}
		for ci, ce := range candidatesY {
			for _, te := range targetsY {
				if d := math.Abs(ce - te); d < threshold && d < bestDY {
					bestDY = d
					snapY = int(te - dh*float64(ci))
				}
			}
		}
	}
	return snapX, snapY
}
