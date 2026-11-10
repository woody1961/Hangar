package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/widget"
)

// resultLine is one line of an import report.
type resultLine struct {
	text   string
	bold   bool
	bullet bool
}

type rsiDialog struct {
	sheet                                                Sheet
	browser                                              string
	open, onRSI, signedIn                                bool
	job                                                  map[string]any
	images                                               map[string]any
	openNote                                             string
	result                                               []resultLine
	resultErr                                            bool
	showBookmark, showPaste                              bool
	paste                                                widget.Editor
	openBtn, cont, forget, fresh, done, copyBm, toggleBm widget.Clickable
	togglePaste, pasteImport, rsiLink                    widget.Clickable
	stop                                                 chan struct{}
	shownJob                                             any
	pasting, opening, imagesRunning, checked             bool
}

func (a *App) openRSI() {
	d := &rsiDialog{stop: make(chan struct{})}
	if job := rsiJobCopy(); job["id"] != nil {
		d.shownJob = job["id"] // a result shown before isn't shown again
	}
	a.rsiDlg = d
	go d.poll(a)
}

func (a *App) closeRSI() {
	if a.rsiDlg != nil {
		close(a.rsiDlg.stop)
		a.rsiDlg = nil
	}
}

func rsiJobCopy() map[string]any {
	rsiJobMu.Lock()
	defer rsiJobMu.Unlock()
	out := map[string]any{}
	for k, v := range rsiJob {
		out[k] = v
	}
	return out
}

// poll checks Hangar's RSI window, the import and the picture downloads every few seconds while the dialog is open.
func (d *rsiDialog) poll(a *App) {
	tick := time.NewTicker(2500 * time.Millisecond)
	defer tick.Stop()
	for {
		var name string
		if b := findBrowser(); b != nil {
			name = b.Name
		}
		st := window.status()
		job := rsiJobCopy()
		img := imageStatus()
		a.post(func() { d.update(a, name, st, job, img) })
		select {
		case <-d.stop:
			return
		case <-tick.C:
		}
	}
}

func (d *rsiDialog) update(a *App, browser string, st, job, img map[string]any) {
	d.browser, d.job, d.images, d.checked = browser, job, img, true
	d.open, d.onRSI, d.signedIn = st["open"] == true, st["on_rsi"] == true, st["signed_in"] == true
	if browser == "" {
		d.showBookmark = true
	}
	state := str(job["state"])
	if job["id"] != nil && job["id"] != d.shownJob && (state == "done" || state == "error") {
		d.shownJob = job["id"]
		if state == "done" {
			a.reload()
			if s, ok := job["summary"].(*importSummary); ok {
				d.showResult(a, s)
			}
		} else {
			d.showError(str(job["error"]))
		}
	}
	d.imagesRunning = img["running"] == true // the window itself refreshes as pictures arrive (watchPictures)
}

func (d *rsiDialog) busy() bool {
	s := str(d.job["state"])
	return s == "reading" || s == "saving" || d.pasting
}

func (d *rsiDialog) showError(msg string) {
	d.result, d.resultErr = []resultLine{{text: msg}}, true
}

func (d *rsiDialog) showResult(a *App, s *importSummary) {
	d.result, d.resultErr = importReport(s), false
}

func jobText(job map[string]any) string {
	if job["state"] == "saving" {
		return "Saving to Hangar…"
	}
	where := "your pledges"
	if job["stage"] == "log" {
		where = "your Hangar log"
	}
	total, _ := job["total"].(int)
	cur, _ := job["current"].(int)
	if total > 0 {
		return fmt.Sprintf("Reading %s: page %d of %d…", where, cur, total)
	}
	return "Reading " + where + "…"
}

// bookmarklet builds the "Import to Hangar" bookmark: the same reader as Hangar's own window, sending to this Hangar.
func bookmarklet() string {
	strip := func(name string) string {
		src, _ := webFiles.ReadFile("web/" + name)
		lines := []string{}
		for _, l := range strings.Split(string(src), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(l), "//") {
				lines = append(lines, l)
			}
		}
		return strings.Join(lines, "\n")
	}
	code := "(function(){\n" + strip("rsi_read.js") + "\n" +
		strings.Replace(strip("rsi_import.js"), "__HANGAR_ORIGIN__", fmt.Sprintf("http://localhost:%d", port), 1) + "\n})();void 0;"
	return "javascript:" + strings.ReplaceAll(url.PathEscape(code), "+", "%2B")
}

func (d *rsiDialog) layout(gtx C, a *App) {
	l := a.look
	busy := d.busy()
	if d.done.Clicked(gtx) && !busy {
		a.closeRSI()
		return
	}
	if d.openBtn.Clicked(gtx) && !d.opening {
		d.opening, d.openNote, d.result = true, "Opening…", nil
		go func() {
			name, err := window.open()
			a.post(func() {
				d.opening = false
				if err != nil {
					d.openNote = ""
					d.showError(errorText(err))
					return
				}
				d.openNote = "Opened in " + name + ". Sign in there if asked."
			})
		}()
	}
	if d.cont.Clicked(gtx) {
		d.result = nil
		rsiJobMu.Lock()
		if s := rsiJob["state"]; s == "reading" || s == "saving" {
			rsiJobMu.Unlock()
		} else {
			rsiJob = map[string]any{"id": float64(time.Now().UnixNano()) / 1e9, "state": "reading", "stage": "pledges", "current": 0, "total": 0}
			d.job = map[string]any{}
			for k, v := range rsiJob {
				d.job[k] = v
			}
			rsiJobMu.Unlock()
			go runRSIJob()
		}
	}
	if d.forget.Clicked(gtx) {
		a.confirmThen("Forget your RSI sign-in?\n\nHangar's RSI window closes and its saved sign-in is deleted. You'd sign in again next time.",
			"Forget", true, func() {
				go func() {
					window.forget()
					a.post(func() { d.openNote = "Signed out of Hangar's window." })
				}()
			})
	}
	if d.fresh.Clicked(gtx) && !busy {
		a.confirmThen("Start fresh?\n\nYour current database and pictures are archived (nothing is deleted) and Hangar begins empty. "+
			"Hangar then opens RSI so you can fill it from RSI only.", "Start fresh", true, func() { d.startFresh(a) })
	}
	if d.toggleBm.Clicked(gtx) {
		d.showBookmark = !d.showBookmark
	}
	if d.togglePaste.Clicked(gtx) {
		d.showPaste = !d.showPaste
	}
	if d.copyBm.Clicked(gtx) {
		gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(bookmarklet()))})
		a.toast("Bookmark copied")
	}
	if d.rsiLink.Clicked(gtx) {
		openBrowser("https://robertsspaceindustries.com/en/account/pledges")
	}
	if d.pasteImport.Clicked(gtx) && !d.pasting {
		d.importPasted(a)
	}

	state := ""
	contOK := false
	switch {
	case !d.checked:
		state = "Checking…"
	case busy && !d.pasting:
		state = jobText(d.job)
	case !d.open:
		state = "Open RSI first (step 1)."
	case !d.signedIn && d.onRSI:
		state = "Waiting for you to sign in to RSI in the Hangar window…"
	case !d.signedIn:
		state = "Waiting for the RSI page to load…"
	default:
		state, contOK = "✓ Signed in. Ready.", true
	}
	if receiverErr != "" {
		state += ""
	}
	noBrowser := d.checked && d.browser == ""
	progress := ""
	if total, _ := d.images["total"].(int); total > 0 {
		done, _ := d.images["done"].(int)
		failed, _ := d.images["failed"].(int)
		progress = fmt.Sprintf("Pictures: %d of %d", done+failed, total)
		if failed > 0 {
			progress += fmt.Sprintf(" (%d failed)", failed)
		}
	}

	step := func(n string, title string, body ...layout.FlexChild) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Bottom: 12}.Layout(gtx, func(gtx C) D {
				return layout.Flex{}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return fixedWidth(gtx, 20, func(gtx C) D { return l.text(gtx, n, textStyle{weight: font.SemiBold}) })
					}),
					layout.Flexed(1, func(gtx C) D {
						kids := []layout.FlexChild{layout.Rigid(func(gtx C) D { return l.text(gtx, title, textStyle{weight: font.SemiBold}) })}
						return column(gtx, append(kids, body...)...)
					}),
				)
			})
		})
	}
	para := func(s string, st textStyle) layout.FlexChild {
		return layout.Rigid(func(gtx C) D { return l.text(gtx, s, st) })
	}
	small := textStyle{size: 12, color: l.p.text2}
	actions := func(btn *widget.Clickable, label string, disabled bool, note string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
				return hrow(gtx,
					layout.Rigid(func(gtx C) D { return l.button(gtx, btn, label, btnOpts{kind: btnPrimary, disabled: disabled}) }),
					hspace(10),
					layout.Flexed(1, func(gtx C) D { return l.text(gtx, note, textStyle{color: l.p.text3}) }),
				)
			})
		})
	}
	disclosure := func(btn *widget.Clickable, open bool, label string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			arrow := "▸ "
			if open {
				arrow = "▾ "
			}
			return layout.Inset{Top: 6, Bottom: 8}.Layout(gtx, func(gtx C) D {
				return btn.Layout(gtx, func(gtx C) D { return l.text(gtx, arrow+label, textStyle{color: l.p.text2}) })
			})
		})
	}

	children := []layout.FlexChild{
		layout.Rigid(func(gtx C) D { return l.heading(gtx, "Import from RSI") }),
		step("1.", "Open your RSI pledges page.",
			para("Hangar opens a browser window just for this. If RSI asks you to sign in, do it there. Hangar never sees your password, and the window remembers you for next time.", textStyle{}),
			actions(&d.openBtn, "Open RSI…", noBrowser || busy || d.opening, d.openNote)),
		step("2.", "When you can see your pledges, come back here and continue.",
			actions(&d.cont, "Continue", noBrowser || busy || !contOK, state)),
	}
	if noBrowser {
		children = append(children, layout.Rigid(func(gtx C) D {
			return background(gtx, l.p.noneBg, rgba(0, 0, 0, 0), 8, pad(8, 12, 8, 12, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return l.text(gtx, "Hangar couldn't find Chrome, Edge or Brave on this computer, so use the bookmark method below.", textStyle{color: l.p.text2})
			}))
		}))
	}
	children = append(children, disclosure(&d.toggleBm, d.showBookmark, "No Chrome, Edge or Brave? Prefer Safari or Firefox? Use a bookmark instead"))
	if d.showBookmark {
		bmNote := "Then in your browser, add a new bookmark (⌘D on a Mac, Ctrl+D on Windows and Linux), edit it, and paste into its address (URL) field. Name it “Import to Hangar”."
		if receiverErr != "" {
			bmNote = "The bookmark can't reach this Hangar right now (" + receiverErr + "), so use the paste method below."
		}
		children = append(children, layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 20}.Layout(gtx, func(gtx C) D {
				return column(gtx,
					step("1.", "One-time setup: copy the bookmark.",
						layout.Rigid(func(gtx C) D {
							return layout.Inset{Top: 8, Bottom: 6}.Layout(gtx, func(gtx C) D {
								return l.button(gtx, &d.copyBm, "Copy the “Import to Hangar” bookmark", btnOpts{kind: btnPrimary})
							})
						}),
						para(bmNote, small)),
					step("2.", "Sign in at RSI's Pledges page and click the bookmark.",
						layout.Rigid(func(gtx C) D {
							return layout.Inset{Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
								return l.button(gtx, &d.rsiLink, "Open robertsspaceindustries.com › Pledges", btnOpts{link: true})
							})
						}),
						para("It reads every page of your pledges and sends them here. The first time, Chrome may ask to allow this site to connect to apps on your device. Choose Allow.", small)),
					disclosure(&d.togglePaste, d.showPaste, "The bookmark says it couldn't reach Hangar?"),
					layout.Rigid(func(gtx C) D {
						if !d.showPaste {
							return D{}
						}
						return column(gtx,
							para("Some browsers (Safari especially) block it. The bookmark copies the data to your clipboard instead. Paste it here:", textStyle{}),
							vspace(6),
							layout.Rigid(func(gtx C) D {
								return l.field(gtx, &d.paste, fieldOpts{hint: "Paste the copied data here", lines: 4, mono: true, small: true})
							}),
							vspace(6),
							layout.Rigid(func(gtx C) D {
								return l.button(gtx, &d.pasteImport, "Import pasted data", btnOpts{kind: btnPrimary, small: true, disabled: d.pasting})
							}),
							vspace(8),
						)
					}),
				)
			})
		}))
	}
	children = append(children,
		para("Pledges already in Hangar are never duplicated. Packages you added by hand are matched by name and get their RSI pledge ID and pictures.", small),
		vspace(10),
		layout.Rigid(func(gtx C) D {
			return hrow(gtx,
				layout.Rigid(func(gtx C) D { return l.button(gtx, &d.fresh, "Start fresh…", btnOpts{small: true, disabled: busy}) }),
				hspace(8),
				layout.Flexed(1, func(gtx C) D {
					return l.text(gtx, "Archives your current data and begins an empty database built only from RSI, including pledges you've melted or gifted, then opens RSI for you.", small)
				}),
			)
		}),
		vspace(14),
		layout.Rigid(func(gtx C) D { return d.resultBox(gtx, a, progress) }),
		vspace(18),
		layout.Rigid(func(gtx C) D {
			return footer(gtx, func(gtx C) D {
				return l.button(gtx, &d.forget, "Forget my RSI sign-in", btnOpts{kind: btnDanger, small: true})
			},
				func(gtx C) D { return l.button(gtx, &d.done, "Done", btnOpts{disabled: busy}) })
		}),
	)
	_, outside := l.sheet(gtx, &d.sheet, 760, func(gtx C) D { return column(gtx, children...) })
	if outside && !busy && a.confirm == nil {
		a.closeRSI()
	}
}

func (d *rsiDialog) resultBox(gtx C, a *App, progress string) D {
	l := a.look
	if len(d.result) == 0 {
		return D{}
	}
	bg, fg := l.p.goodBg, l.p.goodText
	if d.resultErr {
		bg, fg = l.p.noneBg, l.p.danger
	}
	lines := []layout.FlexChild{}
	for i, r := range d.result {
		r := r
		lines = append(lines, layout.Rigid(func(gtx C) D {
			st := textStyle{color: fg}
			if r.bold {
				st.weight = font.SemiBold
			}
			if r.bullet {
				st.color = l.p.text2
				return layout.Inset{Left: 14}.Layout(gtx, func(gtx C) D { return l.text(gtx, "• "+a.disp(r.text), st) })
			}
			return l.text(gtx, a.disp(r.text), st)
		}))
		if i == 0 && progress != "" && !d.resultErr {
			lines = append(lines, layout.Rigid(func(gtx C) D { return l.text(gtx, progress, textStyle{color: fg}) }))
		}
	}
	return background(gtx, bg, rgba(0, 0, 0, 0), 8, pad(10, 14, 10, 14, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return column(gtx, lines...)
	}))
}

func (d *rsiDialog) startFresh(a *App) {
	dbMu.Lock()
	folder, err := freshStart()
	dbMu.Unlock()
	if err != nil {
		d.showError(errorText(err))
		return
	}
	thumbs.forget()
	a.view.Status = ""
	a.reload()
	d.result, d.resultErr = []resultLine{{text: "Archived to " + folder + ". Hangar is empty. Opening RSI for you: sign in there if asked, then click Continue."}}, false
	d.openBtn.Click()
}

func (d *rsiDialog) importPasted(a *App) {
	raw := strings.TrimSpace(d.paste.Text())
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.UseNumber()
	var body map[string]any
	if raw == "" || dec.Decode(&body) != nil {
		d.showError("That doesn't look like the data the bookmark copies. Click the bookmark again and paste straight after.")
		return
	}
	d.pasting = true
	go func() {
		dbMu.Lock()
		summary, tasks, err := importPledges(body["pledges"], body["upgrades"], body["events"], body["complete"] == true)
		dbMu.Unlock()
		if err == nil {
			queueImages(tasks, summary)
		}
		a.post(func() {
			d.pasting = false
			if err != nil {
				d.showError(errorText(err))
				return
			}
			d.paste.SetText("")
			a.reload()
			d.showResult(a, summary)
		})
	}()
}

// importReport turns an import summary into the lines shown under the dialog.
func importReport(s *importSummary) []resultLine {
	pl := func(n int, one, many string) string {
		if n == 1 {
			return one
		}
		return many
	}
	out := []resultLine{{text: fmt.Sprintf("Imported %d pledges: %d new, %d matched packages you already had, %d already imported.",
		s.Pledges, s.Created, s.Matched, s.AlreadyImported), bold: true}}
	if s.ShipIDsRecorded > 0 {
		out = append(out, resultLine{text: fmt.Sprintf("%d ship ID%s recorded (ships you can rename).", s.ShipIDsRecorded, pl(s.ShipIDsRecorded, "", "s"))})
	}
	if s.ImagesSkipped != nil {
		out = append(out, resultLine{text: fmt.Sprintf("%d pictures weren't downloaded (turned off in Settings).", *s.ImagesSkipped)})
	}
	bullets := func(list []string) {
		for _, x := range list {
			out = append(out, resultLine{text: x, bullet: true})
		}
	}
	if u := s.Upgrades; u.Found > 0 {
		bits := []string{}
		if u.Applied > 0 {
			b := fmt.Sprintf("%d upgraded ship%s recorded", u.Applied, pl(u.Applied, "", "s"))
			if u.Renamed > 0 {
				b += fmt.Sprintf(", %d renamed to what you own now", u.Renamed)
			}
			bits = append(bits, b)
		}
		if u.Already > 0 {
			bits = append(bits, fmt.Sprintf("%d already recorded", u.Already))
		}
		if u.Reverted > 0 {
			bits = append(bits, fmt.Sprintf("%d no longer applied", u.Reverted))
		}
		if u.NotInHangar > 0 {
			bits = append(bits, fmt.Sprintf("%d on pledges no longer in your hangar", u.NotInHangar))
		}
		if u.Unresolved > 0 {
			bits = append(bits, fmt.Sprintf("%d couldn't be placed", u.Unresolved))
		}
		t := "nothing to do"
		if len(bits) > 0 {
			t = strings.Join(bits, "; ")
		}
		out = append(out, resultLine{text: "Upgrades: " + t + ".", bold: true})
		bullets(u.Details)
	}
	f := s.Former
	if !f.Checked {
		out = append(out, resultLine{text: "Melted and gifted pledges weren't checked this time (the whole hangar and log weren't read)."})
	} else {
		bits := []string{}
		if f.Melted > 0 {
			bits = append(bits, fmt.Sprintf("%d melted", f.Melted))
		}
		if f.Gifted > 0 {
			bits = append(bits, fmt.Sprintf("%d gifted away", f.Gifted))
		}
		if f.Missing > 0 {
			bits = append(bits, fmt.Sprintf("%d gone with no reason in the log", f.Missing))
		}
		if f.Restored > 0 {
			bits = append(bits, fmt.Sprintf("%d back in your hangar", f.Restored))
		}
		if f.IdentifiedFromLog > 0 {
			bits = append(bits, fmt.Sprintf("%d of those were in your list without a pledge ID", f.IdentifiedFromLog))
		}
		if f.CreatedFromLog > 0 {
			bits = append(bits, fmt.Sprintf("%d added from the log (name and price only; the log doesn't list their items)", f.CreatedFromLog))
		}
		if len(bits) > 0 {
			out = append(out, resultLine{text: "No longer in your hangar: " + strings.Join(bits, "; ") + ". See the sidebar categories.", bold: true})
			bullets(f.Details)
		}
		if len(f.GiftedInHangar) > 0 {
			out = append(out, resultLine{text: "Gifts nobody has claimed yet: RSI still lists these as yours with a Cancel Gift button, so Hangar counts them as yours until the recipient claims them:", bold: true})
			for _, g := range f.GiftedInHangar {
				t := str(g["package"])
				if d := str(g["date"]); d != "" {
					t += " (gifted " + d + ")"
				}
				out = append(out, resultLine{text: t, bullet: true})
			}
		}
		if f.NotFoundTotal > 0 {
			out = append(out, resultLine{text: fmt.Sprintf("%d package%s in your list that RSI doesn't have or remember:", f.NotFoundTotal, pl(f.NotFoundTotal, "", "s")), bold: true})
			bullets(f.NotFound)
		}
	}
	if len(s.InsuranceUnclear) > 0 {
		n := len(s.InsuranceUnclear)
		out = append(out, resultLine{text: fmt.Sprintf("%d package%s list%s more than one insurance term. RSI doesn't say which ship has which, so each ship gets the longest (an upgrade keeps the better cover). Change a ship by hand if you know better; it won't be overwritten.",
			n, pl(n, "", "s"), pl(n, "s", "")), bold: true})
		for _, x := range s.InsuranceUnclear {
			entries, _ := x["entries"].([]string)
			out = append(out, resultLine{text: fmt.Sprintf("%v: %v ships, RSI lists %s → %v", x["package"], x["ships"], strings.Join(entries, ", "), x["used"]), bullet: true})
		}
	}
	if len(s.ShipDifferences) == 0 {
		out = append(out, resultLine{text: "✓ Every ship in your list matches what RSI says you own."})
	} else {
		n := s.ShipDifferencesTotal
		out = append(out, resultLine{text: fmt.Sprintf("%d pledge%s where your ships differ from RSI:", n, pl(n, "", "s")), bold: true})
		for _, x := range s.ShipDifferences {
			rsi, _ := x["rsi"].([]string)
			yours, _ := x["yours"].([]string)
			out = append(out, resultLine{text: fmt.Sprintf("%v: RSI has %s; your list has %s", x["pledge"], orDefault(strings.Join(rsi, ", "), "nothing"),
				orDefault(strings.Join(yours, ", "), "nothing")), bullet: true})
		}
	}
	if s.UnmatchedTotal > 0 {
		out = append(out, resultLine{text: fmt.Sprintf("%d item%s on RSI aren't in the matching packages (not added):", s.UnmatchedTotal, pl(s.UnmatchedTotal, "", "s"))})
		bullets(s.UnmatchedItems)
	}
	if note := pyText(s.LogNote); note != "" {
		out = append(out, resultLine{text: note})
	}
	return out
}
