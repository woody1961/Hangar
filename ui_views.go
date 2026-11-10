package main

import (
	"fmt"
	"image"
	"sort"
	"strings"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// ---------- items table ----------

// widths works out each visible column's width; Name takes what is left, so the table fills the window.
func (a *App) widths(gtx C, cols []*Column, avail int) ([]int, int) {
	ws := make([]int, len(cols))
	total, nameAt := 0, -1
	for i, c := range cols {
		w := c.Width
		if c.Key == "image" {
			w = float32(a.look.tcol)
		}
		ws[i] = gtx.Dp(unit.Dp(w * a.look.fs))
		if c.Key == "image" {
			ws[i] = gtx.Dp(a.look.tcol)
		}
		if c.Key == "name" {
			nameAt = i
		}
		total += ws[i]
	}
	if nameAt >= 0 && total < avail {
		ws[nameAt] += avail - total
		total = avail
	}
	return ws, total
}

func (a *App) itemsView(gtx C) D {
	h, v := a.hangar, &a.view
	cols := a.visibleColumns()
	scope := []*Item{}
	for _, it := range h.Items {
		if h.statusMatches(it, orDefault(v.Status, "owned")) {
			scope = append(scope, it)
		}
	}
	items := h.sortItems(h.filteredItems(v, false, false, ""), v)

	// header clicks: first click sorts ascending, second descending, third goes back to the default order
	for _, c := range cols {
		if c.Get != nil && a.clk("sort|"+c.Key).Clicked(gtx) {
			switch {
			case v.SortKey != c.Key:
				v.SortKey, v.SortDir = c.Key, 1
			case v.SortDir == 1:
				v.SortDir = -1
			default:
				v.SortKey, v.SortDir = "", 1
			}
		}
	}
	for _, it := range items {
		if a.clk(fmt.Sprintf("row|%d", it.ID)).Clicked(gtx) {
			a.openItem(it, 0)
		}
	}

	countText := fmt.Sprintf("%d item%s", len(items), plural(len(items)))
	if len(v.Filters) > 0 {
		countText += " (filtered)"
	}
	return layout.Inset{Left: 18, Right: 18, Top: 6}.Layout(gtx, func(gtx C) D {
		avail := gtx.Constraints.Max.X
		ws, total := a.widths(gtx, cols, avail)
		table := func(gtx C) D {
			gtx.Constraints.Min.X, gtx.Constraints.Max.X = total, total
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 10}.Layout(gtx, func(gtx C) D {
						return a.look.text(gtx, countText, textStyle{size: 12, color: a.look.p.text2})
					})
				}),
				layout.Rigid(func(gtx C) D { return a.tableHead(gtx, cols, ws) }),
				layout.Rigid(func(gtx C) D { return a.tableFilters(gtx, cols, ws, scope) }),
				layout.Rigid(a.look.divider),
				layout.Flexed(1, func(gtx C) D {
					if len(items) == 0 {
						msg := "No items match."
						if v.Query != "" || len(v.Filters) > 0 {
							msg += " Try fewer words, or clear the filters."
						}
						return layout.Inset{Top: 60}.Layout(gtx, func(gtx C) D {
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							return a.look.text(gtx, msg, textStyle{size: 14, color: a.look.p.text2, align: text.Middle})
						})
					}
					return material.List(a.look.th, a.list("main")).Layout(gtx, len(items), func(gtx C, i int) D {
						return a.itemRow(gtx, items[i], cols, ws)
					})
				}),
			)
		}
		if total <= avail {
			return table(gtx)
		}
		// wider than the window: the whole table scrolls sideways
		hl := a.list("hscroll")
		hl.Axis = layout.Horizontal
		return material.List(a.look.th, hl).Layout(gtx, 1, func(gtx C, _ int) D { return table(gtx) })
	})
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func cells(gtx C, ws []int, cell func(gtx C, i int) D, align layout.Alignment) D {
	children := make([]layout.FlexChild, len(ws))
	for i := range ws {
		i := i
		children[i] = layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X, gtx.Constraints.Max.X = ws[i], ws[i]
			dims := cell(gtx, i)
			dims.Size.X = ws[i] // every row keeps the same column edges, whatever is in the cell
			return dims
		})
	}
	return layout.Flex{Alignment: align}.Layout(gtx, children...)
}

func (a *App) tableHead(gtx C, cols []*Column, ws []int) D {
	v := &a.view
	return cells(gtx, ws, func(gtx C, i int) D {
		c := cols[i]
		return layout.Inset{Top: 10, Bottom: 6, Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
			if c.Get == nil {
				return D{Size: image.Pt(gtx.Constraints.Min.X, gtx.Sp(a.look.sp(12)))}
			}
			btn := a.clk("sort|" + c.Key)
			return btn.Layout(gtx, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				col := a.look.p.text2
				if btn.Hovered() {
					col = a.look.p.text
				}
				arrow := ""
				if v.SortKey == c.Key {
					arrow = " ▲"
					if v.SortDir < 0 {
						arrow = " ▼"
					}
				} else if btn.Hovered() {
					arrow = " ↕"
				}
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				align := text.Start
				if c.Right {
					align = text.End
				}
				return a.look.text(gtx, c.Label+arrow, textStyle{size: 12, weight: font.SemiBold, color: col, lines: 1, align: align})
			})
		})
	}, layout.Start)
}

func (a *App) tableFilters(gtx C, cols []*Column, ws []int, scope []*Item) D {
	v := &a.view
	return cells(gtx, ws, func(gtx C, i int) D {
		c := cols[i]
		return layout.Inset{Top: 3, Bottom: 7, Left: 5, Right: 5}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			if c.Get == nil {
				return D{Size: image.Pt(gtx.Constraints.Max.X, 1)}
			}
			if c.Kind == "select" {
				values := map[string]bool{}
				for _, it := range scope {
					if s, _ := c.Get(a.hangar, it).(string); s != "" {
						values[s] = true
					}
				}
				list := make([]string, 0, len(values))
				for s := range values {
					list = append(list, s)
				}
				sort.Slice(list, func(i, j int) bool { return naturalCompare(list[i], list[j]) < 0 })
				opts := []option{{"", "All"}, {blankChoice, "(blank)"}}
				for _, s := range list {
					opts = append(opts, option{s, s})
				}
				dims, val, changed := a.look.dropdown(gtx, a.dd("filter|"+c.Key), opts, v.Filters[c.Key], true, false)
				if changed {
					a.setFilter(c.Key, val)
				}
				return dims
			}
			ed := a.ed("filter|"+c.Key, true)
			for {
				e, ok := ed.Update(gtx)
				if !ok {
					break
				}
				if _, changed := e.(widget.ChangeEvent); changed {
					a.setFilter(c.Key, strings.TrimSpace(ed.Text()))
				}
			}
			if ed.Text() != v.Filters[c.Key] && !gtx.Focused(ed) {
				ed.SetText(v.Filters[c.Key])
			}
			hint, bad := "Filter", false
			if c.Kind == "number" {
				hint = "e.g. >100"
				_, ok := numberFilter(ed.Text())
				bad = !ok
			}
			return a.look.field(gtx, ed, fieldOpts{hint: hint, small: true, bad: bad})
		})
	}, layout.Start)
}

func (a *App) itemRow(gtx C, it *Item, cols []*Column, ws []int) D {
	h := a.hangar
	pkg := h.pkgOf(it)
	btn := a.clk(fmt.Sprintf("row|%d", it.ID))
	vp := unit.Dp(5)
	if a.look.dense {
		vp = 2
	}
	return btn.Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		bg := rgba(0, 0, 0, 0)
		if btn.Hovered() {
			bg = a.look.p.hover
		}
		return background(gtx, bg, rgba(0, 0, 0, 0), 0, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return cells(gtx, ws, func(gtx C, i int) D {
						return layout.Inset{Top: vp, Bottom: vp, Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
							return a.cell(gtx, cols[i], it, pkg)
						})
					}, layout.Middle)
				}),
				layout.Rigid(a.look.divider),
			)
		})
	})
}

func (a *App) cell(gtx C, c *Column, it *Item, pkg *Package) D {
	l := a.look
	if c.Key != "name" && !c.Right {
		gtx.Constraints.Min.X = 0
	}
	muted := textStyle{color: l.p.text3, lines: 2}
	switch c.Key {
	case "image":
		return l.picture(gtx, it, l.tw, l.thh)
	case "name":
		return a.nameBlock(gtx, it, pkg.Name, true)
	case "type":
		if it.Type == "" {
			return D{}
		}
		return l.pill(gtx, it.Type, false)
	case "manufacturer":
		return l.text(gtx, it.Manufacturer, textStyle{lines: 3})
	case "purpose":
		return l.text(gtx, it.Purpose, textStyle{lines: 3})
	case "insurance":
		return l.text(gtx, it.Insurance, muted)
	case "crew":
		return l.text(gtx, it.Crew, textStyle{lines: 2})
	case "storage_scu":
		s := ""
		if it.StorageSCU != "" && it.StorageSCU != "0" {
			s = it.StorageSCU + " SCU"
		}
		return l.text(gtx, s, textStyle{lines: 1, align: text.End})
	case "flight_ready":
		if strings.EqualFold(it.FlightReady, "yes") {
			return l.text(gtx, it.FlightReady, textStyle{color: l.p.goodText, weight: font.SemiBold})
		}
		return l.text(gtx, it.FlightReady, muted)
	case "pledge_date":
		return l.text(gtx, a.disp(pkg.PledgeDate), muted)
	case "purchase_value", "value":
		raw := pkg.PurchaseValue
		if c.Key == "value" {
			raw = pkg.Value
		}
		s := ""
		if m, ok := money(raw); ok {
			s = fmtMoney(m)
		}
		return l.text(gtx, s, textStyle{lines: 1, align: text.End})
	case "pledge_id":
		if pkg.RSIPledgeID == "" {
			return D{}
		}
		return l.text(gtx, "#"+pkg.RSIPledgeID, muted)
	case "membership_id":
		if it.MembershipID == "" {
			return D{}
		}
		return l.text(gtx, "#"+it.MembershipID, muted)
	}
	return D{}
}

// nameBlock: the item's name, the name you gave the ship, what it was upgraded from, and its package.
func (a *App) nameBlock(gtx C, it *Item, sub string, showSub bool) D {
	l := a.look
	children := []layout.FlexChild{
		layout.Rigid(func(gtx C) D { return l.text(gtx, it.Name, textStyle{weight: font.SemiBold, lines: 2}) }),
	}
	if it.CustomName != "" {
		children = append(children, layout.Rigid(func(gtx C) D {
			return l.text(gtx, "“"+it.CustomName+"”", textStyle{size: 12, color: l.p.text2, italic: true, lines: 1})
		}))
	}
	if len(it.History) > 0 {
		children = append(children, layout.Rigid(func(gtx C) D {
			return l.text(gtx, "↑ upgraded from "+it.History[0].From, textStyle{size: 12, color: l.p.accent, lines: 1})
		}))
	}
	if showSub && sub != "" {
		children = append(children, layout.Rigid(func(gtx C) D {
			gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(420))
			return l.text(gtx, a.disp(sub), textStyle{size: 12, color: l.p.text2, lines: 1})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// ---------- packages ----------

func (a *App) packagesView(gtx C) D {
	h, v := a.hangar, &a.view
	l := a.look
	packages := h.visiblePackages(v)
	byPkg := h.itemsByPackage()
	if a.clk("pkg-dir").Clicked(gtx) {
		v.PkgDir = -v.PkgDir
	}
	for _, p := range packages {
		if a.clk(fmt.Sprintf("add|%d", p.ID)).Clicked(gtx) {
			a.openItem(nil, p.ID)
		}
		if a.clk(fmt.Sprintf("edit|%d", p.ID)).Clicked(gtx) {
			a.openPackage(p)
		}
		if a.clk(fmt.Sprintf("secret|%d", p.ID)).Clicked(gtx) {
			a.revealed[p.ID] = !a.revealed[p.ID]
		}
		for _, it := range byPkg[p.ID] {
			if a.clk(fmt.Sprintf("pitem|%d", it.ID)).Clicked(gtx) {
				a.openItem(it, 0)
			}
		}
	}
	sum := func(f func(*Package) float64) float64 {
		t := 0.0
		for _, p := range packages {
			t += f(p)
		}
		return t
	}
	moneyOf := func(s string) float64 { m, _ := money(s); return m }
	totals := fmt.Sprintf("%d package%s · Purchase %s · Upgrades +%s · Current value %s", len(packages), plural(len(packages)),
		fmtMoney(sum(func(p *Package) float64 { return moneyOf(p.PurchaseValue) })),
		fmtMoney(sum(func(p *Package) float64 { return upgradeCost(byPkg[p.ID]) })),
		fmtMoney(sum(func(p *Package) float64 { return moneyOf(p.Value) })))

	dirLabel := "▼ Descending"
	switch {
	case v.PkgSort == "date" && v.PkgDir < 0:
		dirLabel = "▼ Newest first"
	case v.PkgSort == "date":
		dirLabel = "▲ Oldest first"
	case v.PkgDir > 0:
		dirLabel = "▲ Ascending"
	}
	sortBar := func(gtx C) D {
		return layout.Inset{Top: 10}.Layout(gtx, func(gtx C) D {
			return hrow(gtx,
				layout.Rigid(func(gtx C) D { return l.text(gtx, "Sort packages by", textStyle{size: 12, color: l.p.text2}) }),
				hspace(8),
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Max.X = gtx.Dp(170)
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					opts := []option{}
					for _, s := range pkgSorts {
						opts = append(opts, option{s[0], s[1]})
					}
					dims, val, changed := l.dropdown(gtx, a.dd("pkg-sort"), opts, v.PkgSort, true, false)
					if changed {
						v.PkgSort, v.PkgDir = val, -1
						if val == "name" {
							v.PkgDir = 1
						}
					}
					return dims
				}),
				hspace(8),
				layout.Rigid(func(gtx C) D { return l.button(gtx, a.clk("pkg-dir"), dirLabel, btnOpts{small: true}) }),
			)
		})
	}
	n := len(packages) + 2
	return layout.Inset{Left: 18, Right: 18, Top: 6}.Layout(gtx, func(gtx C) D {
		return material.List(l.th, a.list("main")).Layout(gtx, n, func(gtx C, i int) D {
			switch {
			case i == 0:
				return sortBar(gtx)
			case i == 1:
				if len(packages) == 0 {
					return layout.Inset{Top: 60}.Layout(gtx, func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return l.text(gtx, "No packages match.", textStyle{size: 14, color: l.p.text2, align: text.Middle})
					})
				}
				return layout.Inset{Top: 10}.Layout(gtx, func(gtx C) D {
					return l.text(gtx, a.disp(totals), textStyle{size: 12, color: l.p.text2})
				})
			}
			p := packages[i-2]
			return layout.Inset{Top: 12}.Layout(gtx, func(gtx C) D { return a.packageCard(gtx, p, byPkg[p.ID]) })
		})
	})
}

func (a *App) secretChip(gtx C, p *Package) D {
	if p.GiftedTo == "" {
		return D{}
	}
	c := a.clk(fmt.Sprintf("secret|%d", p.ID))
	label := "to ••••••••••• (show)"
	if a.revealed[p.ID] {
		label = "to " + p.GiftedTo
	}
	return c.Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		return a.look.text(gtx, label, textStyle{size: 11, color: a.look.p.text2, weight: font.Medium})
	})
}

// statusPill: "Melted · 2022-11-26 · Store credit $390.00 USD", or a gift nobody has claimed yet.
func (a *App) statusPill(gtx C, p *Package) D {
	l := a.look
	var parts []string
	gift := false
	switch {
	case p.Status != "owned":
		parts = []string{formerInfo[p.Status].label, a.disp(p.StatusDate)}
		gift = true
	case p.GiftPending:
		parts = []string{"Gift not yet claimed"}
		gift = true
	default:
		return D{}
	}
	return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D {
		return background(gtx, l.p.noneBg, rgba(0, 0, 0, 0), 10, pad(1, 8, 1, 8, func(gtx C) D {
			children := []layout.FlexChild{}
			add := func(w layout.Widget) {
				if len(children) > 0 {
					children = append(children, layout.Rigid(func(gtx C) D {
						return l.text(gtx, " · ", textStyle{size: 11, color: l.p.text2})
					}))
				}
				children = append(children, layout.Rigid(w))
			}
			for _, s := range parts {
				s := s
				if s != "" {
					add(func(gtx C) D {
						return l.text(gtx, s, textStyle{size: 11, color: l.p.text2, weight: font.Medium, lines: 1})
					})
				}
			}
			if gift && p.GiftedTo != "" {
				add(func(gtx C) D { return a.secretChip(gtx, p) })
			}
			if p.Status != "owned" && p.StatusNote != "" {
				add(func(gtx C) D {
					return l.text(gtx, a.disp(p.StatusNote), textStyle{size: 11, color: l.p.text2, weight: font.Medium, lines: 1})
				})
			}
			return hrow(gtx, children...)
		}))
	})
}

func (a *App) packageCard(gtx C, p *Package, items []*Item) D {
	l := a.look
	up := upgradeCost(items)
	bought, hasBought := money(p.PurchaseValue)
	now, hasNow := money(p.Value)
	meta := []string{}
	if p.PledgeDate != "" {
		meta = append(meta, p.PledgeDate)
	}
	if hasBought {
		meta = append(meta, "Purchase "+fmtMoney(bought))
	}
	if up > 0 {
		meta = append(meta, "Upgrades +"+fmtMoney(up))
	}
	if hasNow && (!hasBought || now != bought) {
		meta = append(meta, "Value "+fmtMoney(now))
	}
	if p.RSIPledgeID != "" {
		meta = append(meta, "#"+p.RSIPledgeID)
	}
	meta = append(meta, fmt.Sprintf("%d item%s", len(items), plural(len(items))))
	hp := unit.Dp(10)
	ip := unit.Dp(6)
	if l.dense {
		hp, ip = 6, 2
	}
	return background(gtx, rgba(0, 0, 0, 0), l.p.border, 8, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return background(gtx, l.p.hover, rgba(0, 0, 0, 0), 8, func(gtx C) D {
					return layout.Inset{Top: hp, Bottom: hp, Left: 14, Right: 14}.Layout(gtx, func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, func(gtx C) D {
								return column(gtx,
									layout.Rigid(func(gtx C) D {
										return hrow(gtx,
											layout.Flexed(1, func(gtx C) D {
												return l.text(gtx, p.Name, textStyle{size: 14, weight: font.SemiBold, lines: 2})
											}),
											layout.Rigid(func(gtx C) D { return a.statusPill(gtx, p) }),
										)
									}),
									layout.Rigid(func(gtx C) D {
										return l.text(gtx, a.disp(strings.Join(meta, " · ")), textStyle{size: 12, color: l.p.text2, lines: 1})
									}),
								)
							}),
							hspace(10),
							layout.Rigid(func(gtx C) D {
								return l.button(gtx, a.clk(fmt.Sprintf("add|%d", p.ID)), "Add Item", btnOpts{small: true})
							}),
							hspace(10),
							layout.Rigid(func(gtx C) D { return l.button(gtx, a.clk(fmt.Sprintf("edit|%d", p.ID)), "Edit", btnOpts{small: true}) }),
						)
					})
				})
			}),
			layout.Rigid(func(gtx C) D {
				if len(items) == 0 {
					return D{}
				}
				return layout.UniformInset(6).Layout(gtx, func(gtx C) D {
					cell := gtx.Dp(l.tgrid)
					perRow := max(1, gtx.Constraints.Max.X/cell)
					cw := gtx.Constraints.Max.X / perRow
					rows := []layout.FlexChild{}
					for start := 0; start < len(items); start += perRow {
						start := start
						rows = append(rows, layout.Rigid(func(gtx C) D {
							children := []layout.FlexChild{}
							for i := start; i < start+perRow && i < len(items); i++ {
								it := items[i]
								children = append(children, layout.Rigid(func(gtx C) D {
									gtx.Constraints.Min.X, gtx.Constraints.Max.X = cw, cw
									return a.packageItem(gtx, it, ip)
								}))
							}
							return layout.Flex{}.Layout(gtx, children...)
						}))
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
				})
			}),
		)
	})
}

func (a *App) packageItem(gtx C, it *Item, vp unit.Dp) D {
	l := a.look
	c := a.clk(fmt.Sprintf("pitem|%d", it.ID))
	return c.Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		bg := rgba(0, 0, 0, 0)
		if c.Hovered() {
			bg = l.p.hover
		}
		return background(gtx, bg, rgba(0, 0, 0, 0), 6, func(gtx C) D {
			return layout.Inset{Top: vp, Bottom: vp, Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return hrow(gtx,
					layout.Rigid(func(gtx C) D { return l.picture(gtx, it, l.tw, l.thh) }),
					hspace(12),
					layout.Flexed(1, func(gtx C) D {
						sub := strings.Join(nonEmpty(it.Type, it.Manufacturer), " · ")
						return a.nameBlock(gtx, it, sub, true)
					}),
				)
			})
		})
	})
}

func nonEmpty(parts ...string) []string {
	out := []string{}
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ---------- welcome ----------

func (a *App) welcome(gtx C) D {
	l := a.look
	if a.clk("welcome-rsi").Clicked(gtx) {
		a.openRSI()
	}
	if a.clk("welcome-folder").Clicked(gtx) {
		a.useExistingFolder()
	}
	return layout.N.Layout(gtx, func(gtx C) D {
		return layout.Inset{Top: 60}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(520))
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			centered := func(s string, st textStyle) layout.FlexChild {
				return layout.Rigid(func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					st.align = text.Middle
					return l.text(gtx, s, st)
				})
			}
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				centered("Welcome to Hangar", textStyle{size: 22, weight: font.Bold}),
				vspace(12),
				centered("Your hangar is empty. The quickest way to fill it is to import it from your RSI account: every package, its ships "+
					"and items, pictures, upgrades and prices, and anything you've melted or gifted. Nothing to type.", textStyle{size: 14, color: l.p.text2}),
				vspace(14),
				layout.Rigid(func(gtx C) D {
					return l.button(gtx, a.clk("welcome-rsi"), "Import from RSI…", btnOpts{kind: btnPrimary})
				}),
				vspace(14),
				centered("You can also add things by hand with New Package.", textStyle{size: 14, color: l.p.text3}),
				vspace(18),
				centered("Already have Hangar data from before?", textStyle{size: 14, color: l.p.text2}),
				vspace(8),
				layout.Rigid(func(gtx C) D {
					return l.button(gtx, a.clk("welcome-folder"), "Use an existing Hangar data folder…", btnOpts{})
				}),
				vspace(40),
				centered(disclaimer, textStyle{size: 11, color: l.p.text3}),
			)
		})
	})
}

var _ = clip.Rect{}
var _ = paint.Fill
