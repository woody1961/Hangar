package main

// The look of the desktop window: the original Hangar's colours (light and dark), text sizes and corner radii, and the
// controls built from them (buttons, chips, text boxes, check boxes, drop-down menus, dialogs, pictures).

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

type palette struct {
	bg, sidebar, hover, selected, input, sheet, thumb    color.NRGBA
	text, text2, text3, border, borderStrong             color.NRGBA
	accent, accentText, danger, goodBg, goodText, noneBg color.NRGBA
	scrim, mark, sideHover                               color.NRGBA
}

func rgb(c uint32) color.NRGBA {
	return color.NRGBA{R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c), A: 255}
}
func rgba(r, g, b uint8, a float32) color.NRGBA {
	return color.NRGBA{R: r, G: g, B: b, A: uint8(a*255 + 0.5)}
}

var lightPalette = palette{
	bg: rgb(0xffffff), sidebar: rgb(0xf2f2f5), hover: rgba(0, 0, 0, 0.045), selected: rgba(0, 0, 0, 0.08), input: rgb(0xffffff),
	sheet: rgb(0xffffff), thumb: rgb(0xececf0), text: rgb(0x1d1d1f), text2: rgb(0x6e6e73), text3: rgb(0xa1a1a6),
	border: rgba(0, 0, 0, 0.1), borderStrong: rgba(0, 0, 0, 0.18), accent: rgb(0x0071e3), accentText: rgb(0xffffff),
	danger: rgb(0xd70015), goodBg: rgb(0xe8f7ec), goodText: rgb(0x1a7f37), noneBg: rgb(0xf5f5f7), scrim: rgba(0, 0, 0, 0.3),
	mark: rgba(0xff, 0xd6, 0x0a, 0.55),
}

var darkPalette = palette{
	bg: rgb(0x1e1e1e), sidebar: rgb(0x262628), hover: rgba(255, 255, 255, 0.06), selected: rgba(255, 255, 255, 0.11), input: rgb(0x2c2c2e),
	sheet: rgb(0x2a2a2c), thumb: rgb(0x333336), text: rgb(0xf5f5f7), text2: rgb(0xa1a1a6), text3: rgb(0x6e6e73),
	border: rgba(255, 255, 255, 0.1), borderStrong: rgba(255, 255, 255, 0.2), accent: rgb(0x0a84ff), accentText: rgb(0xffffff),
	danger: rgb(0xff453a), goodBg: rgb(0x17331f), goodText: rgb(0x4cd964), noneBg: rgb(0x2c2c2e), scrim: rgba(0, 0, 0, 0.45),
	mark: rgba(0xff, 0xd6, 0x0a, 0.55),
}

// Look holds the current palette and sizes, from Settings.
type Look struct {
	th    *material.Theme
	p     palette
	fs    float32 // font scale: small 0.92, default 1, large 1.15
	tw    unit.Dp // thumbnail width, height, column, grid cell
	thh   unit.Dp
	tcol  unit.Dp
	tgrid unit.Dp
	ticon unit.Sp
	tini  unit.Sp
	dense bool
}

const typeface = "SF Pro Text, SF Pro, Helvetica Neue, Segoe UI, Ubuntu, Noto Sans, Go"

func newLook() *Look {
	th := material.NewTheme()
	th.Face = typeface
	l := &Look{th: th}
	l.apply(settingsValues(), systemIsDark())
	return l
}

func (l *Look) apply(v map[string]any, systemDark bool) {
	dark := v["theme"] == "dark" || (v["theme"] == "system" && systemDark)
	l.p = lightPalette
	if dark {
		l.p = darkPalette
	}
	// The window blends colours differently from a browser, so the see-through greys of the original are mixed into
	// solid colours here, against the backgrounds they sit on.
	raw := l.p
	l.p.hover, l.p.selected = blend(raw.bg, raw.hover), blend(raw.sidebar, raw.selected)
	l.p.sideHover = blend(raw.sidebar, raw.hover)
	l.p.border, l.p.borderStrong = blend(raw.bg, raw.border), blend(raw.bg, raw.borderStrong)
	l.th.Palette = material.Palette{Fg: l.p.text, Bg: l.p.bg, ContrastBg: l.p.accent, ContrastFg: l.p.accentText}
	switch v["font_size"] {
	case "small":
		l.fs = 0.92
	case "large":
		l.fs = 1.15
	default:
		l.fs = 1
	}
	switch v["thumb_size"] {
	case "small":
		l.tw, l.thh, l.tcol, l.tgrid, l.ticon, l.tini = 54, 33, 70, 220, 16, 10
	case "large":
		l.tw, l.thh, l.tcol, l.tgrid, l.ticon, l.tini = 216, 133, 232, 470, 56, 22
	default:
		l.tw, l.thh, l.tcol, l.tgrid, l.ticon, l.tini = 108, 66, 124, 340, 32, 14
	}
	l.dense = v["density"] == "compact"
}

func (l *Look) sp(px float32) unit.Sp { return unit.Sp(px * l.fs) }

// ---------- text ----------

type textStyle struct {
	size   float32
	color  color.NRGBA
	weight font.Weight
	lines  int
	align  text.Alignment
	italic bool
	mono   bool
}

func (l *Look) text(gtx C, s string, st textStyle) D {
	if st.size == 0 {
		st.size = 13
	}
	if st.color == (color.NRGBA{}) {
		st.color = l.p.text
	}
	lb := material.Label(l.th, l.sp(st.size), s)
	lb.Color = st.color
	lb.Font.Weight = st.weight
	lb.Font.Typeface = typeface
	if st.mono {
		lb.Font.Typeface = "SF Mono, Menlo, Consolas, DejaVu Sans Mono, Go Mono"
	}
	if st.italic {
		lb.Font.Style = font.Italic
	}
	lb.MaxLines = st.lines
	lb.Alignment = st.align
	return lb.Layout(gtx)
}

func (l *Look) plain(gtx C, s string) D { return l.text(gtx, s, textStyle{}) }

// ---------- shapes ----------

func fillRect(gtx C, c color.NRGBA, size image.Point, radius int) {
	paint.FillShape(gtx.Ops, c, clip.UniformRRect(image.Rectangle{Max: size}, radius).Op(gtx.Ops))
}

func strokeRect(gtx C, c color.NRGBA, size image.Point, radius int, width float32) {
	paint.FillShape(gtx.Ops, c, clip.Stroke{Path: clip.UniformRRect(image.Rectangle{Max: size}, radius).Path(gtx.Ops), Width: width}.Op())
}

// background draws a rounded box behind w (with an optional border).
func background(gtx C, bg, border color.NRGBA, radius unit.Dp, w layout.Widget) D {
	m := op.Record(gtx.Ops)
	dims := w(gtx)
	call := m.Stop()
	r := gtx.Dp(radius)
	if bg.A > 0 {
		fillRect(gtx, bg, dims.Size, r)
	}
	if border.A > 0 {
		strokeRect(gtx, border, dims.Size, r, float32(gtx.Dp(1)))
	}
	call.Add(gtx.Ops)
	return dims
}

func (l *Look) divider(gtx C) D {
	h := gtx.Dp(1)
	paint.FillShape(gtx.Ops, l.p.border, clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, h)}.Op())
	return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
}

func fixedWidth(gtx C, w unit.Dp, child layout.Widget) D {
	px := gtx.Dp(w)
	gtx.Constraints.Min.X, gtx.Constraints.Max.X = px, px
	return child(gtx)
}

func pad(t, r, b, lft unit.Dp, w layout.Widget) layout.Widget {
	return func(gtx C) D { return layout.Inset{Top: t, Right: r, Bottom: b, Left: lft}.Layout(gtx, w) }
}

func hspace(dp unit.Dp) layout.FlexChild { return layout.Rigid(layout.Spacer{Width: dp}.Layout) }
func vspace(dp unit.Dp) layout.FlexChild { return layout.Rigid(layout.Spacer{Height: dp}.Layout) }

func hrow(gtx C, children ...layout.FlexChild) D {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
}

func column(gtx C, children ...layout.FlexChild) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// ---------- buttons ----------

type btnKind int

const (
	btnNormal btnKind = iota
	btnPrimary
	btnDanger
)

type btnOpts struct {
	kind     btnKind
	small    bool
	link     bool
	disabled bool
}

func (l *Look) button(gtx C, c *widget.Clickable, label string, o btnOpts) D {
	if o.disabled {
		gtx = gtx.Disabled()
	}
	gtx.Constraints.Min = image.Point{} // a button is as wide as its label
	return c.Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		bg, fg, border := l.p.input, l.p.text, l.p.borderStrong
		switch o.kind {
		case btnPrimary:
			bg, fg, border = l.p.accent, l.p.accentText, color.NRGBA{}
		case btnDanger:
			fg = l.p.danger
		}
		if c.Hovered() && !o.disabled {
			if o.kind == btnPrimary {
				bg = lighten(bg, 0.08)
			} else {
				bg = blend(bg, l.p.hover)
			}
		}
		if o.link {
			bg, border = color.NRGBA{}, color.NRGBA{}
		}
		if o.disabled {
			bg, fg, border = fade(bg, 0.45), fade(fg, 0.45), fade(border, 0.45)
		}
		size, vp, hp := float32(13), unit.Dp(5), unit.Dp(12)
		if o.small {
			size, vp, hp = 12, 3, 9
		}
		if o.link {
			vp, hp = 2, 0
		}
		return background(gtx, bg, border, 6, func(gtx C) D {
			return layout.Inset{Top: vp, Bottom: vp, Left: hp, Right: hp}.Layout(gtx, func(gtx C) D {
				return l.text(gtx, label, textStyle{size: size, color: fg, lines: 1})
			})
		})
	})
}

func fade(c color.NRGBA, f float32) color.NRGBA { c.A = uint8(float32(c.A) * f); return c }

func lighten(c color.NRGBA, f float32) color.NRGBA {
	up := func(v uint8) uint8 { return uint8(float32(v) + (255-float32(v))*f) }
	return color.NRGBA{R: up(c.R), G: up(c.G), B: up(c.B), A: c.A}
}

// blend lays a translucent colour over an opaque one.
func blend(base, over color.NRGBA) color.NRGBA {
	a := float32(over.A) / 255
	mix := func(b, o uint8) uint8 { return uint8(float32(b)*(1-a) + float32(o)*a) }
	if base.A == 0 {
		return over
	}
	return color.NRGBA{R: mix(base.R, over.R), G: mix(base.G, over.G), B: mix(base.B, over.B), A: base.A}
}

// chip is a small rounded, clickable tag (a filter you can remove, "Clear all filters").
func (l *Look) chip(gtx C, c *widget.Clickable, label string, danger bool) D {
	return c.Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		bg := l.p.hover
		if c.Hovered() {
			bg = l.p.selected
		}
		fg := l.p.text
		if danger {
			fg = l.p.danger
		}
		return background(gtx, bg, l.p.borderStrong, 12, pad(2, 10, 2, 10, func(gtx C) D {
			return l.text(gtx, label, textStyle{size: 12, color: fg, lines: 1})
		}))
	})
}

// pill is a small grey label (an item's type, a package's former status).
func (l *Look) pill(gtx C, label string, former bool) D {
	gtx.Constraints.Min = image.Point{}
	bg, w := l.p.thumb, font.SemiBold
	if former {
		bg, w = l.p.noneBg, font.Medium
	}
	return background(gtx, bg, color.NRGBA{}, 10, pad(1, 8, 1, 8, func(gtx C) D {
		return l.text(gtx, label, textStyle{size: 11, color: l.p.text2, weight: w, lines: 1})
	}))
}

// ---------- text boxes ----------

type fieldOpts struct {
	hint  string
	bad   bool
	small bool
	lines int // visible lines for a multi-line box
	mono  bool
	blur  bool // hidden until revealed (gift recipients)
}

func (l *Look) field(gtx C, ed *widget.Editor, o fieldOpts) D {
	border := l.p.borderStrong
	if gtx.Focused(ed) {
		border = l.p.accent
	}
	if o.bad {
		border = l.p.danger
	}
	size := float32(13)
	vp, hp := unit.Dp(5), unit.Dp(8)
	if o.small {
		size, vp, hp = 12, 3, 6
	}
	return background(gtx, l.p.input, border, 6, func(gtx C) D {
		return layout.Inset{Top: vp, Bottom: vp, Left: hp, Right: hp}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			if o.lines > 1 {
				h := gtx.Sp(l.sp(size)) * 14 / 10 * o.lines
				gtx.Constraints.Min.Y = h
			}
			e := material.Editor(l.th, ed, o.hint)
			e.TextSize = l.sp(size)
			e.Color = l.p.text
			e.HintColor = l.p.text3
			e.SelectionColor = fade(l.p.accent, 0.35)
			e.Font.Typeface = typeface
			if o.mono {
				e.Font.Typeface = "SF Mono, Menlo, Consolas, DejaVu Sans Mono, Go Mono"
			}
			if o.blur {
				e.Color = l.p.text3
			}
			return e.Layout(gtx)
		})
	})
}

// labelled puts a small grey caption above a control, like the original's forms.
func (l *Look) labelled(gtx C, caption string, w layout.Widget) D {
	return column(gtx,
		layout.Rigid(func(gtx C) D {
			return l.text(gtx, caption, textStyle{size: 12, color: l.p.text2, weight: font.SemiBold})
		}),
		vspace(4),
		layout.Rigid(w),
	)
}

// ---------- check boxes ----------

func (l *Look) checkbox(gtx C, b *widget.Bool, label string, disabled bool) D {
	if disabled {
		gtx = gtx.Disabled()
	}
	return b.Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		return hrow(gtx,
			layout.Rigid(func(gtx C) D {
				s := gtx.Dp(16)
				size := image.Pt(s, s)
				if b.Value {
					fillRect(gtx, l.p.accent, size, gtx.Dp(4))
					layout.Center.Layout(withSize(gtx, size), func(gtx C) D {
						return l.text(gtx, "✓", textStyle{size: 11, color: l.p.accentText, weight: font.Bold})
					})
				} else {
					fillRect(gtx, l.p.input, size, gtx.Dp(4))
					strokeRect(gtx, l.p.borderStrong, size, gtx.Dp(4), float32(gtx.Dp(1)))
				}
				if disabled {
					fillRect(gtx, fade(l.p.bg, 0.5), size, gtx.Dp(4))
				}
				return D{Size: size}
			}),
			hspace(6),
			layout.Rigid(func(gtx C) D {
				c := l.p.text
				if disabled {
					c = l.p.text3
				}
				return l.text(gtx, label, textStyle{color: c})
			}),
		)
	})
}

// ---------- drop-down menus ----------

type option struct{ value, label string }

// Dropdown is a <select>: a box showing the choice, and a list that opens over everything else.
type Dropdown struct {
	btn     widget.Clickable
	outside widget.Clickable
	open    bool
	list    widget.List
	items   []widget.Clickable
}

// dropdown lays out the box; it returns the newly chosen value, if one was chosen this frame.
func (l *Look) dropdown(gtx C, d *Dropdown, options []option, selected string, small, disabled bool) (D, string, bool) {
	if disabled {
		gtx = gtx.Disabled()
		d.open = false
	}
	if d.btn.Clicked(gtx) {
		d.open = !d.open
	}
	if d.outside.Clicked(gtx) {
		d.open = false
	}
	if len(d.items) < len(options) {
		d.items = make([]widget.Clickable, len(options))
	}
	chosen, changed := "", false
	for i := range options {
		if d.items[i].Clicked(gtx) {
			chosen, changed, d.open = options[i].value, true, false
		}
	}
	current := selected
	for _, o := range options {
		if o.value == selected {
			current = o.label
		}
	}
	size := float32(13)
	vp, hp := unit.Dp(5), unit.Dp(8)
	if small {
		size, vp, hp = 12, 3, 6
	}
	dims := d.btn.Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		border := l.p.borderStrong
		if d.open {
			border = l.p.accent
		}
		fg := l.p.text
		if disabled {
			fg = l.p.text3
		}
		return background(gtx, l.p.input, border, 6, func(gtx C) D {
			return layout.Inset{Top: vp, Bottom: vp, Left: hp, Right: hp}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D { return l.text(gtx, current, textStyle{size: size, color: fg, lines: 1}) }),
					layout.Rigid(func(gtx C) D { return l.text(gtx, " ▾", textStyle{size: size, color: l.p.text2}) }),
				)
			})
		})
	})
	if d.open {
		m := op.Record(gtx.Ops)
		// a click anywhere else closes the list
		off := op.Offset(image.Pt(-20000, -20000)).Push(gtx.Ops)
		d.outside.Layout(gtx, func(gtx C) D { return D{Size: image.Pt(40000, 40000)} })
		off.Pop()
		op.Offset(image.Pt(0, dims.Size.Y+gtx.Dp(2))).Add(gtx.Ops)
		mgtx := gtx
		mgtx.Constraints.Min = image.Pt(dims.Size.X, 0)
		mgtx.Constraints.Max = image.Pt(max(dims.Size.X, gtx.Dp(180)), gtx.Dp(320))
		background(mgtx, l.p.sheet, l.p.borderStrong, 6, func(gtx C) D {
			d.list.Axis = layout.Vertical
			return material.List(l.th, &d.list).Layout(gtx, len(options), func(gtx C, i int) D {
				return d.items[i].Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					bg := color.NRGBA{}
					if d.items[i].Hovered() {
						bg = l.p.accent
					}
					fg := l.p.text
					if bg.A > 0 {
						fg = l.p.accentText
					}
					return background(gtx, bg, color.NRGBA{}, 4, pad(4, 10, 4, 10, func(gtx C) D {
						w := font.Normal
						if options[i].value == selected {
							w = font.SemiBold
						}
						return l.text(gtx, options[i].label, textStyle{size: size, color: fg, weight: w, lines: 1})
					}))
				})
			})
		})
		op.Defer(gtx.Ops, m.Stop())
	}
	return dims, chosen, changed
}

// ---------- dialogs ----------

// Sheet is a dialog's frame: a dimmed window behind a centred card whose content scrolls when it is taller than the window.
type Sheet struct {
	scrim widget.Clickable
	block widget.Clickable
	list  widget.List
}

// sheet lays out a dialog; it reports whether the dimmed area outside it was clicked.
func (l *Look) sheet(gtx C, s *Sheet, width unit.Dp, content layout.Widget) (D, bool) {
	outside := s.scrim.Clicked(gtx)
	full := gtx.Constraints.Max
	s.scrim.Layout(gtx, func(gtx C) D {
		paint.FillShape(gtx.Ops, l.p.scrim, clip.Rect{Max: full}.Op())
		return D{Size: full}
	})
	layout.Center.Layout(gtx, func(gtx C) D {
		w := min(gtx.Dp(width), full.X-gtx.Dp(32))
		gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
		gtx.Constraints.Min.Y = 0
		gtx.Constraints.Max.Y = full.Y - gtx.Dp(60)
		// clicks on the card itself must not reach the dimmed area under it
		return s.block.Layout(gtx, func(gtx C) D {
			return background(gtx, l.p.sheet, l.p.border, 12, func(gtx C) D {
				s.list.Axis = layout.Vertical
				return material.List(l.th, &s.list).Layout(gtx, 1, func(gtx C, _ int) D {
					return layout.Inset{Top: 20, Bottom: 16, Left: 22, Right: 22}.Layout(gtx, content)
				})
			})
		})
	})
	return D{Size: full}, outside
}

func (l *Look) heading(gtx C, s string) D {
	return layout.Inset{Bottom: 16}.Layout(gtx, func(gtx C) D {
		return l.text(gtx, s, textStyle{size: 16, weight: font.Bold})
	})
}

// ---------- pictures ----------

// picture draws an item's picture (or its placeholder) in a rounded box of the given size.
func (l *Look) picture(gtx C, it *Item, w, h unit.Dp) D {
	size := image.Pt(gtx.Dp(w), gtx.Dp(h))
	r := gtx.Dp(8)
	fillRect(gtx, l.p.thumb, size, r)
	if it.Image != "" {
		if img, ok := thumbs.get(it.Image, size); ok {
			defer clip.UniformRRect(image.Rectangle{Max: size}, r).Push(gtx.Ops).Pop()
			gtx.Constraints = layout.Exact(size)
			widget.Image{Src: img, Fit: widget.Cover, Position: layout.Center, Scale: 1 / gtx.Metric.PxPerDp}.Layout(gtx)
			return D{Size: size}
		}
		return D{Size: size}
	}
	symbol, icon := placeholder(it)
	ts := l.tini
	if icon {
		ts = l.ticon
	}
	layout.Center.Layout(withSize(gtx, size), func(gtx C) D {
		lb := material.Label(l.th, unit.Sp(float32(ts)*l.fs), symbol)
		lb.Color = l.p.text3
		lb.Font.Weight = font.Bold
		if icon {
			lb.Font.Weight = font.Normal
		}
		return lb.Layout(gtx)
	})
	return D{Size: size}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func withSize(gtx C, size image.Point) C {
	gtx.Constraints = layout.Exact(size)
	return gtx
}
