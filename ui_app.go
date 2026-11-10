package main

// The desktop window: sidebar, toolbar and the Items / Packages views, with the dialogs on top.

import (
	"fmt"
	"image"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/explorer"
)

type App struct {
	w        *app.Window
	look     *Look
	hangar   *Hangar
	view     View
	settings map[string]any
	explorer *explorer.Explorer

	clicks map[string]*widget.Clickable
	eds    map[string]*widget.Editor
	drops  map[string]*Dropdown
	bools  map[string]*widget.Bool
	lists  map[string]*widget.List

	search   widget.Editor
	revealed map[int64]bool

	itemDlg     *itemDialog
	pkgDlg      *packageDialog
	settingsDlg *settingsDialog
	rsiDlg      *rsiDialog
	confirm     *confirmDialog

	toastText  string
	toastUntil time.Time

	mu      sync.Mutex
	pending []func()
	fatal   string
}

func newApp(w *app.Window) *App {
	a := &App{w: w, clicks: map[string]*widget.Clickable{}, eds: map[string]*widget.Editor{}, drops: map[string]*Dropdown{},
		bools: map[string]*widget.Bool{}, lists: map[string]*widget.List{}, revealed: map[int64]bool{}}
	a.search.SingleLine = true
	a.settings = settingsValues()
	a.look = newLook()
	a.explorer = explorer.NewExplorer(w)
	a.view = View{Mode: str(a.settings["start_view"]), Filters: map[string]string{}, SortDir: 1, PkgSort: "date", PkgDir: -1}
	if a.view.Mode == "" {
		a.view.Mode = "items"
	}
	thumbs.invalidate = w.Invalidate
	return a
}

// ---------- plumbing ----------

func (a *App) clk(key string) *widget.Clickable {
	c := a.clicks[key]
	if c == nil {
		c = new(widget.Clickable)
		a.clicks[key] = c
	}
	return c
}

func (a *App) ed(key string, single bool) *widget.Editor {
	e := a.eds[key]
	if e == nil {
		e = &widget.Editor{SingleLine: single, Submit: single}
		a.eds[key] = e
	}
	return e
}

func (a *App) dd(key string) *Dropdown {
	d := a.drops[key]
	if d == nil {
		d = new(Dropdown)
		a.drops[key] = d
	}
	return d
}

func (a *App) boolean(key string) *widget.Bool {
	b := a.bools[key]
	if b == nil {
		b = new(widget.Bool)
		a.bools[key] = b
	}
	return b
}

func (a *App) list(key string) *widget.List {
	l := a.lists[key]
	if l == nil {
		l = &widget.List{List: layout.List{Axis: layout.Vertical}}
		a.lists[key] = l
	}
	return l
}

// post runs fn on the window's own goroutine at the next frame (work done in the background reports back this way).
func (a *App) post(fn func()) {
	a.mu.Lock()
	a.pending = append(a.pending, fn)
	a.mu.Unlock()
	a.w.Invalidate()
}

func (a *App) runPending() {
	a.mu.Lock()
	fns := a.pending
	a.pending = nil
	a.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

func (a *App) toast(msg string) {
	a.toastText, a.toastUntil = msg, time.Now().Add(2200*time.Millisecond)
	a.w.Invalidate()
}

// reload reads the database again and keeps the view sensible (a type that no longer has items, an empty category).
func (a *App) reload() {
	h, err := loadHangar()
	if err != nil {
		a.fatal = "Hangar couldn't read its database: " + err.Error()
		return
	}
	a.hangar = h
	owned := h.owned()
	has := func(f func(*Item) bool) bool {
		for _, it := range owned {
			if f(it) {
				return true
			}
		}
		return false
	}
	if a.view.Type != "" && !has(func(it *Item) bool { return orDefault(it.Type, "Other") == a.view.Type }) {
		a.view.Type = ""
	}
	if a.view.Purpose != "" && !has(func(it *Item) bool { return orDefault(it.Purpose, noPurpose) == a.view.Purpose }) {
		a.view.Purpose = ""
	}
	if a.view.Status != "" {
		found := false
		for _, p := range h.Packages {
			if p.Status == a.view.Status {
				found = true
			}
		}
		if !found {
			a.view.Status = ""
		}
	}
}

func (a *App) applySettings(v map[string]any) {
	a.settings = v
	a.look.apply(v, systemIsDark())
	a.w.Invalidate()
}

// ---------- what is shown: date format and privacy ----------

var (
	amountRe  = regexp.MustCompile(`\$\d[\d,]*(?:\.\d+)?(?: [A-Z]{3})?`)
	idRe      = regexp.MustCompile(`#\d{5,}`)
	isoDateRe = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
	months    = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
)

// disp applies the date format and the privacy settings to text about to be shown.
func (a *App) disp(s string) string {
	if a.settings["date_format"] == "long" {
		s = isoDateRe.ReplaceAllStringFunc(s, func(d string) string {
			m := isoDateRe.FindStringSubmatch(d)
			var mo, day int
			fmt.Sscan(m[2], &mo)
			fmt.Sscan(m[3], &day)
			if mo < 1 || mo > 12 {
				return d
			}
			return fmt.Sprintf("%s %d, %s", months[mo-1], day, m[1])
		})
	}
	if a.settings["hide_amounts"] == true {
		s = amountRe.ReplaceAllString(s, "$•••••")
	}
	if a.settings["hide_ids"] == true {
		s = idRe.ReplaceAllString(s, "#•••••••")
	}
	return s
}

func (a *App) privacyHidden() []string {
	out := []string{}
	if a.settings["hide_amounts"] == true {
		out = append(out, "purchase_value", "value")
	}
	if a.settings["hide_ids"] == true {
		out = append(out, "pledge_id", "membership_id")
	}
	return out
}

// visibleColumns: the columns chosen in Settings (Name and the picture always stay), minus those privacy removes.
func (a *App) visibleColumns() []*Column {
	hidden := map[string]bool{}
	if l, ok := a.settings["hidden_columns"].([]string); ok {
		for _, k := range l {
			hidden[k] = true
		}
	}
	for _, k := range a.privacyHidden() {
		hidden[k] = true
	}
	out := []*Column{}
	for i := range columns {
		if !hidden[columns[i].Key] {
			out = append(out, &columns[i])
		}
	}
	return out
}

// ---------- the frame ----------

func (a *App) frame(gtx C) {
	a.runPending()
	paint.Fill(gtx.Ops, a.look.p.bg)
	if a.fatal != "" {
		layout.Center.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(560))
			return a.look.text(gtx, a.fatal, textStyle{size: 15, color: a.look.p.danger})
		})
		return
	}
	a.shortcuts(gtx)
	layout.Flex{}.Layout(gtx,
		layout.Rigid(a.sidebar),
		layout.Rigid(func(gtx C) D {
			w := gtx.Dp(1)
			paint.FillShape(gtx.Ops, a.look.p.border, clip.Rect{Max: image.Pt(w, gtx.Constraints.Max.Y)}.Op())
			return D{Size: image.Pt(w, gtx.Constraints.Max.Y)}
		}),
		layout.Flexed(1, a.main),
	)
	switch {
	case a.itemDlg != nil:
		a.itemDlg.layout(gtx, a)
	case a.pkgDlg != nil:
		a.pkgDlg.layout(gtx, a)
	case a.settingsDlg != nil:
		a.settingsDlg.layout(gtx, a)
	case a.rsiDlg != nil:
		a.rsiDlg.layout(gtx, a)
	}
	if a.confirm != nil {
		a.confirm.layout(gtx, a)
	}
	a.layoutToast(gtx)
}

// shortcuts: / or ⌘K / Ctrl+K jumps to the search box; Esc closes a dialog or clears the search.
func (a *App) shortcuts(gtx C) {
	for {
		e, ok := gtx.Event(key.Filter{Name: "K", Required: key.ModShortcut}, key.Filter{Name: "/"}, key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		ke, isKey := e.(key.Event)
		if !isKey || ke.State != key.Press {
			continue
		}
		dialogOpen := a.itemDlg != nil || a.pkgDlg != nil || a.settingsDlg != nil || a.rsiDlg != nil || a.confirm != nil
		switch ke.Name {
		case key.NameEscape:
			switch {
			case a.confirm != nil:
				a.confirm = nil
			case a.itemDlg != nil:
				a.itemDlg = nil
			case a.pkgDlg != nil:
				a.pkgDlg = nil
			case a.settingsDlg != nil:
				a.settingsDlg = nil
			case a.rsiDlg != nil && !a.rsiDlg.busy():
				a.closeRSI()
			case a.view.Query != "":
				a.search.SetText("")
				a.view.Query = ""
			}
		default:
			if !dialogOpen {
				gtx.Execute(key.FocusCmd{Tag: &a.search})
				a.search.SetCaret(a.search.Len(), 0)
			}
		}
	}
}

func (a *App) layoutToast(gtx C) {
	if a.toastText == "" || time.Now().After(a.toastUntil) {
		a.toastText = ""
		return
	}
	gtx.Execute(op.InvalidateCmd{At: a.toastUntil})
	layout.S.Layout(gtx, func(gtx C) D {
		return layout.Inset{Bottom: 24}.Layout(gtx, func(gtx C) D {
			return background(gtx, a.look.p.text, rgba(0, 0, 0, 0), 20, pad(8, 16, 8, 16, func(gtx C) D {
				return a.look.text(gtx, a.toastText, textStyle{color: a.look.p.bg})
			}))
		})
	})
}

// ---------- sidebar ----------

func (a *App) navItem(gtx C, key, label string, n int, active, muted bool, onClick func()) D {
	c := a.clk("nav|" + key)
	if c.Clicked(gtx) {
		onClick()
		a.list("main").Position = layout.Position{}
	}
	return c.Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		bg := rgba(0, 0, 0, 0)
		if active {
			bg = a.look.p.selected
		} else if c.Hovered() {
			bg = a.look.p.sideHover
		}
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return background(gtx, bg, rgba(0, 0, 0, 0), 6, pad(5, 10, 5, 10, func(gtx C) D {
			col, w := a.look.p.text, font.Normal
			if muted {
				col = a.look.p.text2
			}
			if active {
				w = font.SemiBold
			}
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D { return a.look.text(gtx, label, textStyle{color: col, weight: w, lines: 1}) }),
				layout.Rigid(func(gtx C) D {
					if n < 0 {
						return D{}
					}
					return a.look.text(gtx, fmt.Sprint(n), textStyle{size: 12, color: a.look.p.text3})
				}),
			)
		}))
	})
}

func (a *App) navHeading(gtx C, s string) D {
	return layout.Inset{Top: 14, Bottom: 4, Left: 10, Right: 10}.Layout(gtx, func(gtx C) D {
		return a.look.text(gtx, s, textStyle{size: 11, weight: font.SemiBold, color: a.look.p.text3})
	})
}

func (a *App) sidebar(gtx C) D {
	width := gtx.Dp(220)
	gtx.Constraints.Min.X, gtx.Constraints.Max.X = width, width
	paint.FillShape(gtx.Ops, a.look.p.sidebar, clip.Rect{Max: image.Pt(width, gtx.Constraints.Max.Y)}.Op())
	h := a.hangar
	owned := h.owned()
	v := &a.view
	pkgCount := 0
	for _, p := range h.Packages {
		if p.Status == "owned" {
			pkgCount++
		}
	}
	listMax := gtx.Constraints.Max.Y * 30 / 100
	navList := func(key string, counts []count, kind string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			gtx.Constraints.Max.Y = listMax
			l := a.list("side-" + key)
			return material.List(a.look.th, l).Layout(gtx, len(counts), func(gtx C, i int) D {
				c := counts[i]
				active := v.Mode == "items" && v.Status == "" && ((kind == "type" && v.Type == c.Name && v.Purpose == "") ||
					(kind == "purpose" && v.Purpose == c.Name && v.Type == ""))
				return a.navItem(gtx, kind+"|"+c.Name, c.Name, c.N, active, false, func() {
					*v = View{Mode: "items", Query: v.Query, Filters: v.Filters, SortKey: v.SortKey, SortDir: v.SortDir, PkgSort: v.PkgSort, PkgDir: v.PkgDir}
					if kind == "type" {
						v.Type = c.Name
					} else {
						v.Purpose = c.Name
					}
				})
			})
		})
	}
	return layout.Inset{Top: 14, Bottom: 14, Left: 10, Right: 10}.Layout(gtx, func(gtx C) D {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 4, Left: 10, Right: 10, Bottom: 12}.Layout(gtx, func(gtx C) D {
					return a.look.text(gtx, "Hangar", textStyle{size: 17, weight: font.Bold})
				})
			}),
			layout.Rigid(func(gtx C) D { return a.navHeading(gtx, "Library") }),
			layout.Rigid(func(gtx C) D {
				active := v.Mode == "items" && v.Status == "" && v.Type == "" && v.Purpose == ""
				return a.navItem(gtx, "all", "All Items", len(owned), active, false, func() {
					v.Mode, v.Status, v.Type, v.Purpose = "items", "", "", ""
				})
			}),
			layout.Rigid(func(gtx C) D {
				return a.navItem(gtx, "packages", "Packages", pkgCount, v.Mode == "packages" && v.Status == "", false, func() {
					v.Mode, v.Status, v.Type, v.Purpose = "packages", "", "", ""
				})
			}),
		}
		// Former categories only appear once something is in them
		for _, s := range formerStatuses {
			s := s
			n := 0
			for _, p := range h.Packages {
				if p.Status == s {
					n++
				}
			}
			if n == 0 {
				continue
			}
			children = append(children, layout.Rigid(func(gtx C) D {
				return a.navItem(gtx, "status|"+s, formerInfo[s].nav, n, v.Mode == "packages" && v.Status == s, true, func() {
					v.Mode, v.Status, v.Type, v.Purpose = "packages", s, "", ""
				})
			}))
		}
		children = append(children,
			layout.Rigid(func(gtx C) D { return a.navHeading(gtx, "Types") }),
			navList("types", countBy(owned, func(it *Item) string { return orDefault(it.Type, "Other") }), "type"),
			layout.Rigid(func(gtx C) D { return a.navHeading(gtx, "Purpose") }),
			navList("purposes", countBy(owned, func(it *Item) string { return orDefault(it.Purpose, noPurpose) }), "purpose"),
			layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(0, gtx.Constraints.Min.Y)} }),
			layout.Rigid(func(gtx C) D { return a.navHeading(gtx, "Export") }),
			layout.Rigid(func(gtx C) D { return a.navItem(gtx, "settings", "Settings…", -1, false, false, a.openSettings) }),
			layout.Rigid(func(gtx C) D {
				return a.navItem(gtx, "csv", "Export as CSV", -1, false, false, func() { a.export("csv") })
			}),
			layout.Rigid(func(gtx C) D {
				return a.navItem(gtx, "json", "Full backup (JSON)", -1, false, false, func() { a.export("json") })
			}),
		)
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// ---------- toolbar and main area ----------

func (a *App) main(gtx C) D {
	for {
		e, ok := a.search.Update(gtx)
		if !ok {
			break
		}
		if _, changed := e.(widget.ChangeEvent); changed {
			a.view.Query = a.search.Text()
		}
	}
	if a.clk("open-rsi").Clicked(gtx) {
		a.openRSI()
	}
	if a.clk("new-item").Clicked(gtx) {
		a.openItem(nil, 0)
	}
	if a.clk("new-package").Clicked(gtx) {
		a.openPackage(nil)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 12, Bottom: 12, Left: 18, Right: 18}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(380))
						gtx.Constraints.Min.X = min(gtx.Dp(200), gtx.Constraints.Max.X)
						return a.searchBox(gtx)
					}),
					layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
					layout.Rigid(func(gtx C) D {
						return a.look.button(gtx, a.clk("open-rsi"), "Import from RSI", btnOpts{kind: btnPrimary})
					}),
					hspace(8),
					layout.Rigid(func(gtx C) D { return a.look.button(gtx, a.clk("new-item"), "New Item", btnOpts{}) }),
					hspace(8),
					layout.Rigid(func(gtx C) D { return a.look.button(gtx, a.clk("new-package"), "New Package", btnOpts{}) }),
				)
			})
		}),
		layout.Rigid(a.look.divider),
		layout.Rigid(a.ownership),
		layout.Rigid(a.filterBar),
		layout.Flexed(1, func(gtx C) D {
			switch {
			case len(a.hangar.Packages) == 0:
				return a.welcome(gtx)
			case a.view.Mode == "packages":
				return a.packagesView(gtx)
			}
			return a.itemsView(gtx)
		}),
	)
}

func (a *App) searchBox(gtx C) D {
	border := a.look.p.borderStrong
	if gtx.Focused(&a.search) {
		border = a.look.p.accent
	}
	return background(gtx, a.look.p.input, border, 8, func(gtx C) D {
		return layout.Inset{Top: 7, Bottom: 7, Left: 9, Right: 8}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return magnifier(gtx, a.look.p.text3) }),
				hspace(7),
				layout.Flexed(1, func(gtx C) D {
					e := material.Editor(a.look.th, &a.search, "Search everything…")
					e.TextSize = a.look.sp(14)
					e.Color, e.HintColor = a.look.p.text, a.look.p.text3
					e.SelectionColor = fade(a.look.p.accent, 0.35)
					e.Font.Typeface = typeface
					return e.Layout(gtx)
				}),
				layout.Rigid(func(gtx C) D {
					if gtx.Focused(&a.search) || a.search.Len() > 0 {
						return D{}
					}
					return background(gtx, rgba(0, 0, 0, 0), a.look.p.border, 4, pad(0, 5, 0, 5, func(gtx C) D {
						return a.look.text(gtx, "/", textStyle{size: 11, color: a.look.p.text3, mono: true})
					}))
				}),
			)
		})
	})
}

// magnifier draws the search icon: a ring and a handle.
func magnifier(gtx C, c rgbaColor) D {
	s := gtx.Dp(14)
	r := float32(s) * 0.33
	center := image.Pt(int(float32(s)*0.42), int(float32(s)*0.42))
	ring := clip.Ellipse{Min: center.Sub(image.Pt(int(r), int(r))), Max: center.Add(image.Pt(int(r), int(r)))}
	paint.FillShape(gtx.Ops, c, clip.Stroke{Path: ring.Path(gtx.Ops), Width: float32(gtx.Dp(1.6))}.Op())
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32pt(float32(center.X)+r*0.75, float32(center.Y)+r*0.75))
	p.LineTo(f32pt(float32(s)*0.95, float32(s)*0.95))
	paint.FillShape(gtx.Ops, c, clip.Stroke{Path: p.End(), Width: float32(gtx.Dp(1.8))}.Op())
	return D{Size: image.Pt(s, s)}
}

func (a *App) ownership(gtx C) D {
	msg, owned := a.hangar.ownershipText(&a.view)
	if msg == "" {
		return D{}
	}
	bg, fg, w := a.look.p.noneBg, a.look.p.text2, font.Normal
	if owned {
		bg, fg, w = a.look.p.goodBg, a.look.p.goodText, font.SemiBold
	}
	return layout.Inset{Top: 12, Left: 18, Right: 18}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return background(gtx, bg, rgba(0, 0, 0, 0), 8, pad(10, 14, 10, 14, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return a.look.text(gtx, a.disp(msg), textStyle{size: 14, color: fg, weight: w})
		}))
	})
}

// filterBar shows what is filtering or sorting right now, each removable with one click.
func (a *App) filterBar(gtx C) D {
	v := &a.view
	type chip struct {
		key, label string
		danger     bool
		do         func()
	}
	chips := []chip{}
	keys := make([]string, 0, len(v.Filters))
	for _, c := range columns {
		if _, ok := v.Filters[c.Key]; ok {
			keys = append(keys, c.Key)
		}
	}
	for _, k := range keys {
		k := k
		val := v.Filters[k]
		if val == blankChoice {
			val = "(blank)"
		}
		chips = append(chips, chip{"f|" + k, columnByKey(k).Label + ": " + val + " ✕", false, func() { a.setFilter(k, "") }})
	}
	if c := columnByKey(v.SortKey); c != nil && v.Mode == "items" {
		arrow := "▲"
		if v.SortDir < 0 {
			arrow = "▼"
		}
		chips = append(chips, chip{"sort", "Sorted by " + c.Label + " " + arrow + " ✕", false, func() { v.SortKey, v.SortDir = "", 1 }})
	}
	if len(v.Filters) > 1 {
		chips = append(chips, chip{"all", "Clear all filters", true, func() {
			for k := range v.Filters {
				a.setFilter(k, "")
			}
		}})
	}
	if len(chips) == 0 {
		return D{}
	}
	for _, c := range chips {
		if a.clk("chip|" + c.key).Clicked(gtx) {
			c.do()
		}
	}
	return layout.Inset{Top: 8, Left: 18, Right: 18}.Layout(gtx, func(gtx C) D {
		children := []layout.FlexChild{}
		for _, c := range chips {
			c := c
			children = append(children, layout.Rigid(func(gtx C) D { return a.look.chip(gtx, a.clk("chip|"+c.key), c.label, c.danger) }), hspace(6))
		}
		return layout.Flex{}.Layout(gtx, children...)
	})
}

func (a *App) setFilter(key, value string) {
	if value == "" {
		delete(a.view.Filters, key)
		if e := a.eds["filter|"+key]; e != nil && e.Text() != "" {
			e.SetText("")
		}
	} else {
		a.view.Filters[key] = value
	}
}

// ---------- exports ----------

// export saves the CSV (one row per item, for spreadsheets) or the full JSON backup where the person chooses.
func (a *App) export(kind string) {
	name := "ships-export.csv"
	if kind == "json" {
		name = "ships-export.json"
	}
	go func() {
		var data []byte
		var err error
		if kind == "csv" {
			var b strings.Builder
			err = writeCSV(&b)
			data = []byte(b.String())
		} else {
			data, err = exportJSON()
		}
		if err != nil {
			a.post(func() { a.toast("Export failed: " + err.Error()) })
			return
		}
		out, err := a.explorer.CreateFile(name)
		if err == explorer.ErrNotAvailable {
			path := downloadsPath(name)
			if err = os.WriteFile(path, data, 0o644); err == nil {
				a.post(func() { a.toast("Saved " + path) })
				revealFile(path)
			}
		} else if err == nil {
			_, err = out.Write(data)
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			if err == nil {
				a.post(func() { a.toast("Saved " + name) })
			}
		}
		if err != nil && err != explorer.ErrUserDecline {
			a.post(func() { a.toast("Couldn't save the export: " + err.Error()) })
		}
	}()
}

// watchPictures shows pictures as they arrive after an import, whatever is open in the window, and says when they
// are all in (or how many couldn't be downloaded; importing again retries those).
func (a *App) watchPictures() {
	st := imageStatus()
	seenDone, _ := st["done"].(int)
	seenFailed, _ := st["failed"].(int)
	batchFailed := 0
	for {
		time.Sleep(1500 * time.Millisecond)
		st := imageStatus()
		done, _ := st["done"].(int)
		failed, _ := st["failed"].(int)
		if done == seenDone && failed == seenFailed {
			continue // nothing new
		}
		batchFailed += failed - seenFailed
		seenDone, seenFailed = done, failed
		if st["running"] == true {
			a.post(a.reload) // show what has arrived so far
			continue
		}
		gone := batchFailed
		batchFailed = 0
		a.post(func() {
			a.reload()
			if gone > 0 {
				a.toast(fmt.Sprintf("Pictures downloaded. %d couldn't be downloaded; import again to retry them.", gone))
			} else {
				a.toast("Pictures downloaded")
			}
		})
	}
}

// ---------- running the window ----------

func runWindow() {
	w := new(app.Window)
	w.Option(app.Title("Hangar"), app.Size(unit.Dp(1280), unit.Dp(860)), app.MinSize(unit.Dp(860), unit.Dp(560)))
	a := newApp(w)
	if fatalStart != "" {
		a.fatal = fatalStart
	} else {
		a.reload()
		startReceiver(a)
		go a.watchPictures()
	}
	go func() {
		var ops op.Ops
		for {
			e := w.Event()
			a.explorer.ListenEvents(e)
			switch e := e.(type) {
			case app.DestroyEvent:
				window.close() // Hangar's RSI window closes with it (its sign-in is kept)
				os.Exit(0)
			case app.FrameEvent:
				gtx := app.NewContext(&ops, e)
				a.frame(gtx)
				e.Frame(gtx.Ops)
			}
		}
	}()
	app.Main()
}
