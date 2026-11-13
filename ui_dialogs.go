package main

import (
	"fmt"
	"image"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
)

// ---------- confirm ----------

type confirmDialog struct {
	sheet       Sheet
	message, ok string
	danger      bool
	run         func()
	yes, no     widget.Clickable
}

func (a *App) confirmThen(message, ok string, danger bool, run func()) {
	a.confirm = &confirmDialog{message: message, ok: ok, danger: danger, run: run}
}

func (d *confirmDialog) layout(gtx C, a *App) {
	l := a.look
	if d.no.Clicked(gtx) {
		a.confirm = nil
		return
	}
	if d.yes.Clicked(gtx) {
		a.confirm = nil
		d.run()
		return
	}
	_, outside := l.sheet(gtx, &d.sheet, 440, func(gtx C) D {
		return column(gtx,
			layout.Rigid(func(gtx C) D { return l.text(gtx, d.message, textStyle{size: 14}) }),
			vspace(18),
			layout.Rigid(func(gtx C) D {
				kind := btnPrimary
				if d.danger {
					kind = btnDanger
				}
				return footer(gtx, nil, func(gtx C) D { return l.button(gtx, &d.no, "Cancel", btnOpts{}) },
					func(gtx C) D { return l.button(gtx, &d.yes, d.ok, btnOpts{kind: kind}) })
			}),
		)
	})
	if outside {
		a.confirm = nil
	}
}

// footer: an optional button on the left, then the rest on the right.
func footer(gtx C, left layout.Widget, right ...layout.Widget) D {
	children := []layout.FlexChild{}
	if left != nil {
		children = append(children, layout.Rigid(left))
	}
	children = append(children, layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }))
	for i, w := range right {
		if i > 0 {
			children = append(children, hspace(8))
		}
		children = append(children, layout.Rigid(w))
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
}

// grid2 lays fields out two per row; a wide field takes the whole row.
type gridField struct {
	wide bool
	w    layout.Widget
}

func grid2(gtx C, fields ...gridField) D {
	rows := []layout.FlexChild{}
	for i := 0; i < len(fields); {
		if len(rows) > 0 {
			rows = append(rows, vspace(10))
		}
		f := fields[i]
		if f.wide || i+1 == len(fields) || fields[i+1].wide {
			rows = append(rows, layout.Rigid(func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return f.w(gtx)
			}))
			i++
			continue
		}
		g := fields[i+1]
		rows = append(rows, layout.Rigid(func(gtx C) D {
			return layout.Flex{}.Layout(gtx,
				layout.Flexed(1, f.w),
				hspace(12),
				layout.Flexed(1, g.w),
			)
		}))
		i += 2
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

// ---------- item editor ----------

type itemDialog struct {
	sheet                                                     Sheet
	item                                                      *Item
	name, typ, maker, purpose, insurance, crew, scu, notes, u widget.Editor
	pkgID                                                     int64
	flight                                                    string
	pkgDrop, flightDrop, typeDrop, makerDrop, purposeDrop     Dropdown
	choose, fetch, remove, del, cancel, save                  widget.Clickable
	newData                                                   []byte
	newType                                                   string
	removeImage                                               bool
	preview                                                   *paint.ImageOp
	errText                                                   string
	busy                                                      bool
}

func singleLine(e *widget.Editor, s string) {
	e.SingleLine, e.Submit = true, true
	e.SetText(s)
}

func (a *App) openItem(it *Item, packageID int64) {
	d := &itemDialog{item: it, pkgID: packageID}
	for _, e := range []*widget.Editor{&d.name, &d.typ, &d.maker, &d.purpose, &d.insurance, &d.crew, &d.scu, &d.u} {
		singleLine(e, "")
	}
	if it != nil {
		d.name.SetText(it.Name)
		d.typ.SetText(it.Type)
		d.maker.SetText(it.Manufacturer)
		d.purpose.SetText(it.Purpose)
		d.insurance.SetText(it.Insurance)
		d.crew.SetText(it.Crew)
		d.scu.SetText(it.StorageSCU)
		d.notes.SetText(it.Notes)
		d.pkgID, d.flight = it.PackageID, it.FlightReady
		if it.Image != "" {
			name := it.Image
			go func() {
				if img, err := decodeFile(imageDirPath(name)); err == nil {
					op := paint.NewImageOp(contain(img, image.Pt(660, 495)))
					a.post(func() {
						if a.itemDlg == d && d.preview == nil && !d.removeImage {
							d.preview = &op
						}
					})
				}
			}()
		}
	}
	a.itemDlg = d
}

func (d *itemDialog) layout(gtx C, a *App) {
	l := a.look
	h := a.hangar
	if d.cancel.Clicked(gtx) {
		a.itemDlg = nil
		return
	}
	if d.save.Clicked(gtx) && !d.busy {
		d.saveItem(a)
	}
	for _, e := range []*widget.Editor{&d.name, &d.typ, &d.maker, &d.purpose, &d.insurance, &d.crew, &d.scu} {
		for {
			ev, ok := e.Update(gtx)
			if !ok {
				break
			}
			if _, submit := ev.(widget.SubmitEvent); submit && !d.busy {
				d.saveItem(a)
			}
		}
	}
	if d.del.Clicked(gtx) && d.item != nil {
		it := d.item
		a.confirmThen(fmt.Sprintf("Delete “%s”?", it.Name), "Delete", true, func() {
			dbMu.Lock()
			err := deleteItem(store, it.ID)
			dbMu.Unlock()
			if err != nil {
				d.errText = errorText(err)
				return
			}
			a.itemDlg = nil
			a.reload()
			a.toast("Item deleted")
		})
	}
	if d.choose.Clicked(gtx) {
		go func() {
			rc, err := a.explorer.ChooseFile(".jpg", ".jpeg", ".png", ".webp", ".gif")
			if err != nil {
				return
			}
			data, err := io.ReadAll(io.LimitReader(rc, 50<<20))
			rc.Close()
			var small []byte
			var ctype string
			if err == nil {
				small, ctype, err = shrinkForStorage(data)
			}
			a.post(func() {
				if err != nil {
					d.errText = "Couldn't read that picture."
					return
				}
				d.setPicture(small, ctype)
			})
		}()
	}
	if d.fetch.Clicked(gtx) {
		u := strings.TrimSpace(d.u.Text())
		if !httpURLPrefix.MatchString(u) {
			d.errText = "Enter an image URL starting with http:// or https://"
		} else {
			d.errText = "Fetching the picture…"
			go func() {
				data, ctype, err := downloadImage(u)
				if err == nil && imageTypes[strings.ToLower(strings.TrimSpace(strings.Split(ctype, ";")[0]))] == "" {
					err = userErr("Unsupported image type")
				}
				if err == nil && len(data) > maxImageBytes {
					err = userErr("Image is empty or larger than 5 MB")
				}
				a.post(func() {
					if err != nil {
						d.errText = "Could not download image: " + errorText(err)
						return
					}
					d.errText = ""
					d.setPicture(data, ctype)
				})
			}()
		}
	}
	if d.remove.Clicked(gtx) {
		d.preview, d.newData, d.removeImage = nil, nil, true
	}

	pkgOpts := []option{{"0", "(No package)"}}
	for _, p := range h.Packages {
		pkgOpts = append(pkgOpts, option{fmt.Sprint(p.ID), p.Name})
	}
	known := func(get func(*Item) string) []option {
		seen := map[string]bool{}
		for _, it := range h.Items {
			if s := get(it); s != "" {
				seen[s] = true
			}
		}
		out := []option{}
		for s := range seen {
			out = append(out, option{s, s})
		}
		sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].value) < strings.ToLower(out[j].value) })
		return out
	}
	withSuggestions := func(e *widget.Editor, dd *Dropdown, opts []option) layout.Widget {
		return func(gtx C) D {
			return hrow(gtx,
				layout.Flexed(1, func(gtx C) D { return l.field(gtx, e, fieldOpts{}) }),
				hspace(4),
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(34), gtx.Dp(34)
					dims, v, changed := l.dropdown(gtx, dd, opts, "", false, len(opts) == 0)
					if changed {
						e.SetText(v)
					}
					return dims
				}),
			)
		}
	}

	title := "New Item"
	if d.item != nil {
		title = "Edit Item"
	}
	_, outside := l.sheet(gtx, &d.sheet, 760, func(gtx C) D {
		return column(gtx,
			layout.Rigid(func(gtx C) D { return l.heading(gtx, title) }),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return fixedWidth(gtx, 220, func(gtx C) D { return d.pictureColumn(gtx, a) }) }),
					hspace(20),
					layout.Flexed(1, func(gtx C) D {
						return grid2(gtx,
							gridField{true, func(gtx C) D {
								return l.labelled(gtx, "Name", func(gtx C) D { return l.field(gtx, &d.name, fieldOpts{}) })
							}},
							gridField{true, func(gtx C) D {
								return l.labelled(gtx, "Package", func(gtx C) D {
									dims, v, changed := l.dropdown(gtx, &d.pkgDrop, pkgOpts, fmt.Sprint(d.pkgID), false, false)
									if changed {
										fmt.Sscan(v, &d.pkgID)
									}
									return dims
								})
							}},
							gridField{false, func(gtx C) D {
								return l.labelled(gtx, "Type", withSuggestions(&d.typ, &d.typeDrop, known(func(it *Item) string { return it.Type })))
							}},
							gridField{false, func(gtx C) D {
								return l.labelled(gtx, "Manufacturer", withSuggestions(&d.maker, &d.makerDrop, known(func(it *Item) string { return it.Manufacturer })))
							}},
							gridField{false, func(gtx C) D {
								return l.labelled(gtx, "Purpose", withSuggestions(&d.purpose, &d.purposeDrop, known(func(it *Item) string { return it.Purpose })))
							}},
							gridField{false, func(gtx C) D {
								return l.labelled(gtx, "Insurance", func(gtx C) D { return l.field(gtx, &d.insurance, fieldOpts{}) })
							}},
							gridField{false, func(gtx C) D {
								return l.labelled(gtx, "Crew", func(gtx C) D { return l.field(gtx, &d.crew, fieldOpts{}) })
							}},
							gridField{false, func(gtx C) D {
								return l.labelled(gtx, "Storage (SCU)", func(gtx C) D { return l.field(gtx, &d.scu, fieldOpts{}) })
							}},
							gridField{false, func(gtx C) D {
								return l.labelled(gtx, "Flight ready", func(gtx C) D {
									opts := []option{{"", " "}, {"Yes", "Yes"}, {"In Concept", "In Concept"}}
									if d.flight != "" && d.flight != "Yes" && d.flight != "In Concept" {
										opts = append(opts, option{d.flight, d.flight}) // keep a value the list doesn't offer
									}
									dims, v, changed := l.dropdown(gtx, &d.flightDrop, opts, d.flight, false, false)
									if changed {
										d.flight = v
									}
									return dims
								})
							}},
							gridField{true, func(gtx C) D {
								return l.labelled(gtx, "Notes", func(gtx C) D { return l.field(gtx, &d.notes, fieldOpts{lines: 3}) })
							}},
							gridField{true, func(gtx C) D { return d.idsBlock(gtx, a) }},
							gridField{true, func(gtx C) D { return d.historyBlock(gtx, a) }},
						)
					}),
				)
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 10}.Layout(gtx, func(gtx C) D { return l.text(gtx, d.errText, textStyle{color: l.p.danger}) })
			}),
			vspace(18),
			layout.Rigid(func(gtx C) D {
				var left layout.Widget
				if d.item != nil {
					left = func(gtx C) D { return l.button(gtx, &d.del, "Delete", btnOpts{kind: btnDanger}) }
				}
				return footer(gtx, left, func(gtx C) D { return l.button(gtx, &d.cancel, "Cancel", btnOpts{}) },
					func(gtx C) D { return l.button(gtx, &d.save, "Save", btnOpts{kind: btnPrimary, disabled: d.busy}) })
			}),
		)
	})
	if outside {
		a.itemDlg = nil
	}
}

func imageDirPath(name string) string { return imageDir + string(osSep) + baseName(name) }

func (d *itemDialog) setPicture(data []byte, ctype string) {
	img, err := decodePicture(data)
	if err != nil {
		d.errText = errorText(err)
		return
	}
	op := paint.NewImageOp(contain(img, image.Pt(660, 495)))
	d.preview, d.newData, d.newType, d.removeImage = &op, data, ctype, false
}

func (d *itemDialog) pictureColumn(gtx C, a *App) D {
	l := a.look
	return column(gtx,
		layout.Rigid(func(gtx C) D {
			return d.choose.Layout(gtx, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				w := gtx.Constraints.Max.X
				size := image.Pt(w, w*3/4)
				border := l.p.borderStrong
				if d.choose.Hovered() {
					border = l.p.accent
				}
				fillRect(gtx, l.p.thumb, size, gtx.Dp(10))
				strokeRect(gtx, border, size, gtx.Dp(10), float32(gtx.Dp(2)))
				g := gtx
				g.Constraints = layout.Exact(size)
				if d.preview != nil {
					layout.UniformInset(2).Layout(g, func(gtx C) D {
						return widget.Image{Src: *d.preview, Fit: widget.Contain, Position: layout.Center}.Layout(gtx)
					})
				} else {
					layout.Center.Layout(g, func(gtx C) D {
						return column(gtx,
							layout.Rigid(func(gtx C) D { return l.text(gtx, "Click to choose a picture", textStyle{color: l.p.text2}) }),
							layout.Rigid(func(gtx C) D {
								return l.text(gtx, "or fetch one from a web address", textStyle{size: 12, color: l.p.text3})
							}),
						)
					})
				}
				return D{Size: size}
			})
		}),
		vspace(8),
		layout.Rigid(func(gtx C) D {
			return hrow(gtx,
				layout.Flexed(1, func(gtx C) D { return l.field(gtx, &d.u, fieldOpts{hint: "…or paste an image URL", small: true}) }),
				hspace(6),
				layout.Rigid(func(gtx C) D { return l.button(gtx, &d.fetch, "Fetch", btnOpts{small: true}) }),
			)
		}),
		layout.Rigid(func(gtx C) D {
			if d.preview == nil {
				return D{}
			}
			return layout.Inset{Top: 6}.Layout(gtx, func(gtx C) D {
				return l.button(gtx, &d.remove, "Remove picture", btnOpts{kind: btnDanger, small: true, link: true})
			})
		}),
	)
}

func (d *itemDialog) idsBlock(gtx C, a *App) D {
	l := a.look
	it := d.item
	if it == nil {
		return D{}
	}
	owner := a.hangar.byID[it.PackageID]
	if it.MembershipID == "" && (owner == nil || owner.RSIPledgeID == "") {
		return D{}
	}
	lines := []layout.FlexChild{}
	if it.MembershipID != "" {
		s := "Ship ID #" + it.MembershipID
		if it.CustomName != "" {
			s += "   named “" + it.CustomName + "”"
		}
		lines = append(lines, layout.Rigid(func(gtx C) D { return l.text(gtx, a.disp(s), textStyle{size: 12}) }))
	}
	if owner != nil && owner.RSIPledgeID != "" {
		lines = append(lines, layout.Rigid(func(gtx C) D {
			return l.text(gtx, a.disp("Pledge ID #"+owner.RSIPledgeID+"   ("+owner.Name+")"), textStyle{size: 12})
		}))
	}
	lines = append(lines, layout.Rigid(func(gtx C) D {
		return l.text(gtx, "RSI gives IDs to pledges, to upgrades and to ships you can rename. Other ships and items have none, so they are tracked by their pledge.",
			textStyle{size: 12, color: l.p.text3})
	}))
	return column(gtx, lines...)
}

func (d *itemDialog) historyBlock(gtx C, a *App) D {
	l := a.look
	it := d.item
	if it == nil || len(it.History) == 0 {
		return D{}
	}
	lines := []layout.FlexChild{layout.Rigid(func(gtx C) D {
		return l.text(gtx, "Upgrade history", textStyle{size: 12, weight: font.SemiBold, color: l.p.text2})
	})}
	for i, s := range it.History {
		i, s := i, s
		bits := []string{}
		if s.Date != "" {
			bits = append(bits, s.Date[:min(10, len(s.Date))])
		}
		if s.UpgradeID != "" {
			bits = append(bits, "upgrade #"+s.UpgradeID)
		}
		if s.Price != "" {
			m, _ := money(s.Price)
			bits = append(bits, "upgrade "+fmtMoney(m))
		}
		if s.Value != "" {
			bits = append(bits, "pledge then worth "+s.Value)
		}
		lines = append(lines, layout.Rigid(func(gtx C) D {
			return hrow(gtx,
				layout.Rigid(func(gtx C) D {
					return l.text(gtx, fmt.Sprintf("%d. %s → %s  ", i+1, s.From, s.To), textStyle{size: 12, weight: font.SemiBold})
				}),
				layout.Flexed(1, func(gtx C) D {
					return l.text(gtx, a.disp(strings.Join(bits, " · ")), textStyle{size: 12, color: l.p.text3})
				}),
			)
		}))
	}
	if c := upgradeCost([]*Item{it}); c > 0 {
		lines = append(lines, layout.Rigid(func(gtx C) D {
			return l.text(gtx, a.disp("Upgrades cost "+fmtMoney(c)+" in total"), textStyle{size: 12, weight: font.SemiBold})
		}))
	}
	if b := it.History[0]; b.BeforeName != "" {
		s := "Your list said “" + b.BeforeName + "”"
		if b.BeforePurpose != "" {
			s += " (purpose: " + b.BeforePurpose + ")"
		}
		s += "; the old details were cleared because they described that ship."
		lines = append(lines, layout.Rigid(func(gtx C) D { return l.text(gtx, s, textStyle{size: 12, color: l.p.text3}) }))
	}
	return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D { return column(gtx, lines...) })
}

func (d *itemDialog) saveItem(a *App) {
	data := map[string]any{"name": d.name.Text(), "type": d.typ.Text(), "manufacturer": d.maker.Text(), "purpose": d.purpose.Text(),
		"insurance": d.insurance.Text(), "crew": d.crew.Text(), "storage_scu": d.scu.Text(), "flight_ready": d.flight, "notes": d.notes.Text()}
	if d.pkgID != 0 {
		data["package_id"] = float64(d.pkgID)
	} else {
		data["package_id"] = nil
	}
	wasNew := d.item == nil
	dbMu.Lock()
	defer dbMu.Unlock()
	var saved row
	var err error
	if wasNew {
		saved, err = createItem(store, data)
	} else {
		saved, err = updateItem(store, d.item.ID, data)
	}
	if err == nil {
		id := num(saved["id"])
		// if the picture fails below, saving again edits this item instead of adding a second one
		d.item = &Item{ID: id, Name: txt(saved["name"]), PackageID: num(saved["package_id"]), Image: txt(saved["image"])}
		switch {
		case d.newData != nil:
			_, err = saveItemImage(id, d.newData, d.newType)
		case d.removeImage && txt(saved["image"]) != "":
			_, err = setItemImage(store, id, nil)
		}
	}
	if err != nil {
		d.errText = errorText(err)
		return
	}
	a.itemDlg = nil
	a.post(func() {
		a.reload()
		if wasNew {
			a.toast("Item added")
		} else {
			a.toast("Item saved")
		}
	})
}

// ---------- package editor ----------

type packageDialog struct {
	sheet                                                       Sheet
	pkg                                                         *Package
	name, pledgeID, date, purchase, value, statusDate, giftedTo widget.Editor
	statusNote, notes                                           widget.Editor
	status                                                      string
	statusDrop                                                  Dropdown
	masked                                                      bool
	reveal, del, cancel, save                                   widget.Clickable
	errText                                                     string
}

var statusOptions = []option{{"owned", "In my hangar"}, {"melted", "Melted (reclaimed for credit)"}, {"gifted", "Gifted away"},
	{"missing", "No longer in hangar"}}

func (a *App) openPackage(p *Package) {
	d := &packageDialog{pkg: p, status: "owned", masked: true}
	for _, e := range []*widget.Editor{&d.name, &d.pledgeID, &d.date, &d.purchase, &d.value, &d.statusDate, &d.giftedTo, &d.statusNote} {
		singleLine(e, "")
	}
	if p != nil {
		d.name.SetText(p.Name)
		d.pledgeID.SetText(p.RSIPledgeID)
		d.date.SetText(p.PledgeDate)
		d.purchase.SetText(p.PurchaseValue)
		d.value.SetText(p.Value)
		d.statusDate.SetText(p.StatusDate)
		d.giftedTo.SetText(p.GiftedTo)
		d.statusNote.SetText(p.StatusNote)
		d.notes.SetText(p.Notes)
		d.status = p.Status
	} else {
		d.date.SetText(time.Now().Format("2006-01-02")) // a package added by hand starts with today's date, so it lands at the top
	}
	a.pkgDlg = d
}

var dateRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})?$`)

func (d *packageDialog) layout(gtx C, a *App) {
	l := a.look
	if d.cancel.Clicked(gtx) {
		a.pkgDlg = nil
		return
	}
	if d.save.Clicked(gtx) {
		d.savePackage(a)
	}
	for _, e := range []*widget.Editor{&d.name, &d.pledgeID, &d.date, &d.purchase, &d.value, &d.statusDate, &d.statusNote} {
		for {
			ev, ok := e.Update(gtx)
			if !ok {
				break
			}
			if _, submit := ev.(widget.SubmitEvent); submit {
				d.savePackage(a)
			}
		}
	}
	if d.reveal.Clicked(gtx) {
		d.masked = !d.masked
	}
	if d.del.Clicked(gtx) && d.pkg != nil {
		p := d.pkg
		n := 0
		for _, it := range a.hangar.Items {
			if it.PackageID == p.ID {
				n++
			}
		}
		a.confirmThen(fmt.Sprintf("Delete “%s” and its %d item%s?", p.Name, n, plural(n)), "Delete", true, func() {
			dbMu.Lock()
			err := deletePackage(store, p.ID)
			dbMu.Unlock()
			if err != nil {
				d.errText = errorText(err)
				return
			}
			a.pkgDlg = nil
			a.reload()
			a.toast("Package deleted")
		})
	}
	// Who a gift went to stays hidden (safe to stream) and read-only until you choose to show it.
	d.giftedTo.ReadOnly = d.masked
	d.giftedTo.Mask = 0
	if d.masked {
		d.giftedTo.Mask = '•'
	}
	privAmounts := a.settings["hide_amounts"] == true
	for _, e := range []*widget.Editor{&d.purchase, &d.value, &d.statusNote} {
		e.Mask = 0
		if privAmounts {
			e.Mask = '•'
		}
	}
	d.pledgeID.Mask = 0
	if a.settings["hide_ids"] == true {
		d.pledgeID.Mask = '•'
	}
	f := func(caption string, e *widget.Editor, o fieldOpts) layout.Widget {
		return func(gtx C) D { return l.labelled(gtx, caption, func(gtx C) D { return l.field(gtx, e, o) }) }
	}
	title := "New Package"
	if d.pkg != nil {
		title = "Edit Package"
	}
	revealLabel := "Show"
	if !d.masked {
		revealLabel = "Hide"
	}
	_, outside := l.sheet(gtx, &d.sheet, 480, func(gtx C) D {
		return column(gtx,
			layout.Rigid(func(gtx C) D { return l.heading(gtx, title) }),
			layout.Rigid(func(gtx C) D {
				return grid2(gtx,
					gridField{true, f("Package name", &d.name, fieldOpts{})},
					gridField{false, f("RSI pledge ID", &d.pledgeID, fieldOpts{})},
					gridField{false, f("Pledge date", &d.date, fieldOpts{hint: "YYYY-MM-DD"})},
					gridField{false, f("Purchase value", &d.purchase, fieldOpts{hint: "$0.00 USD"})},
					gridField{false, f("Current value", &d.value, fieldOpts{hint: "$0.00 USD"})},
					gridField{false, func(gtx C) D {
						return l.labelled(gtx, "Status", func(gtx C) D {
							dims, v, changed := l.dropdown(gtx, &d.statusDrop, statusOptions, d.status, false, false)
							if changed {
								d.status = v
							}
							return dims
						})
					}},
					gridField{false, f("Status date", &d.statusDate, fieldOpts{hint: "YYYY-MM-DD"})},
					gridField{true, func(gtx C) D {
						return l.labelled(gtx, "Gifted to", func(gtx C) D {
							return hrow(gtx,
								layout.Flexed(1, func(gtx C) D {
									return l.field(gtx, &d.giftedTo, fieldOpts{hint: "Email of the person it was gifted to", blur: d.masked})
								}),
								hspace(6),
								layout.Rigid(func(gtx C) D { return l.button(gtx, &d.reveal, revealLabel, btnOpts{small: true}) }),
							)
						})
					}},
					gridField{true, f("Status note", &d.statusNote, fieldOpts{hint: "e.g. Store credit $390.00 USD"})},
					gridField{true, f("Notes", &d.notes, fieldOpts{lines: 3})},
				)
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 10}.Layout(gtx, func(gtx C) D { return l.text(gtx, d.errText, textStyle{color: l.p.danger}) })
			}),
			vspace(18),
			layout.Rigid(func(gtx C) D {
				var left layout.Widget
				if d.pkg != nil {
					left = func(gtx C) D { return l.button(gtx, &d.del, "Delete", btnOpts{kind: btnDanger}) }
				}
				return footer(gtx, left, func(gtx C) D { return l.button(gtx, &d.cancel, "Cancel", btnOpts{}) },
					func(gtx C) D { return l.button(gtx, &d.save, "Save", btnOpts{kind: btnPrimary}) })
			}),
		)
	})
	if outside {
		a.pkgDlg = nil
	}
}

func (d *packageDialog) savePackage(a *App) {
	for _, e := range []*widget.Editor{&d.date, &d.statusDate} {
		if !dateRe.MatchString(strings.TrimSpace(e.Text())) {
			d.errText = "Dates are written as YYYY-MM-DD, for example 2026-10-14."
			return
		}
	}
	data := map[string]any{"name": d.name.Text(), "rsi_pledge_id": d.pledgeID.Text(), "pledge_date": d.date.Text(),
		"purchase_value": d.purchase.Text(), "value": d.value.Text(), "status": d.status, "status_date": d.statusDate.Text(),
		"gifted_to": d.giftedTo.Text(), "status_note": d.statusNote.Text(), "notes": d.notes.Text()}
	dbMu.Lock()
	var saved row
	var err error
	if d.pkg == nil {
		saved, err = createPackage(store, data)
	} else {
		saved, err = updatePackage(store, d.pkg.ID, data)
	}
	dbMu.Unlock()
	if err != nil {
		d.errText = errorText(err)
		return
	}
	a.pkgDlg = nil
	a.reload()
	if d.pkg == nil {
		a.toast("Package created — now add its items")
		a.openItem(nil, num(saved["id"]))
	} else {
		a.toast("Package saved")
	}
}

var _ unit.Dp
