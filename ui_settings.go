package main

import (
	"fmt"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
)

type settingsDialog struct {
	sheet                                                      Sheet
	msg                                                        string
	msgErr                                                     bool
	backups                                                    []map[string]any
	theme, fontSize, thumb, density, dateFmt, startOn, browse  Dropdown
	port, daily, snapshots, movePath                           widget.Editor
	restart, pick, move, delPrev, backupNow, openBackups, done widget.Clickable
	restore                                                    map[string]*widget.Clickable
	busy                                                       bool
}

func (a *App) openSettings() {
	a.settings = settingsValues()
	d := &settingsDialog{restore: map[string]*widget.Clickable{}}
	singleLine(&d.port, fmt.Sprint(a.settings["port"]))
	singleLine(&d.daily, fmt.Sprint(a.settings["backups_daily"]))
	singleLine(&d.snapshots, fmt.Sprint(a.settings["backups_snapshots"]))
	singleLine(&d.movePath, "")
	a.settingsDlg = d
	d.refreshBackups(a)
}

func (d *settingsDialog) refreshBackups(a *App) {
	go func() {
		dbMu.Lock()
		list := listBackups()
		dbMu.Unlock()
		a.post(func() { d.backups = list })
	}()
}

func (d *settingsDialog) say(text string, isErr bool) { d.msg, d.msgErr = text, isErr }

// save stores one or more settings at once; the screen follows what was really saved.
func (d *settingsDialog) save(a *App, changes map[string]any) {
	if p, ok := changes["port"].(int); ok && p != port && !portIsFree(p) {
		d.say(fmt.Sprintf("Port %d is already in use by another program. Choose a different port.", p), true)
		return
	}
	v, err := updateSettings(changes)
	if err != nil {
		d.say(errorText(err), true)
		a.applySettings(settingsValues())
		return
	}
	d.say("", false)
	a.applySettings(v)
	for _, k := range a.privacyHidden() {
		delete(a.view.Filters, k)
		if a.view.SortKey == k {
			a.view.SortKey, a.view.SortDir = "", 1
		}
	}
	if l, ok := v["hidden_columns"].([]string); ok && contains(l, a.view.SortKey) {
		a.view.SortKey, a.view.SortDir = "", 1
	}
}

func (d *settingsDialog) restartNeeded(a *App) bool {
	v := a.settings
	_, envPort := activeEnvOverrides()["port"]
	_, envData := activeEnvOverrides()["data_dir"]
	if v["port"] != port && !envPort {
		return true
	}
	want, _ := v["data_dir"].(string)
	return want != "" && !envData && realPath(expandUser(want)) != realPath(dataDir)
}

func (d *settingsDialog) layout(gtx C, a *App) {
	l := a.look
	v := a.settings
	env := activeEnvOverrides()
	if d.done.Clicked(gtx) {
		a.settingsDlg = nil
		return
	}
	if d.restart.Clicked(gtx) {
		a.restartApp()
	}
	if d.backupNow.Clicked(gtx) {
		dbMu.Lock()
		_, err := snapshot("manual", 0)
		dbMu.Unlock()
		if err != nil {
			d.say(errorText(err), true)
		} else {
			d.say("Backed up.", false)
		}
		d.refreshBackups(a)
	}
	if d.openBackups.Clicked(gtx) {
		if err := openFolder(backupDir); err != nil {
			d.say(errorText(err), true)
		}
	}
	if d.pick.Clicked(gtx) && !d.busy {
		d.say("Waiting for you to choose a folder…", false)
		go func() {
			path, err := pickFolder()
			a.post(func() {
				switch {
				case err != nil:
					d.say(errorText(err), true)
				case path != "":
					d.movePath.SetText(path)
					d.say("Now click Move data… to copy your data there.", false)
				default:
					d.say("", false)
				}
			})
		}()
	}
	if d.move.Clicked(gtx) && !d.busy {
		path := strings.TrimSpace(d.movePath.Text())
		if path == "" {
			d.say("Type the folder to move your data to.", true)
		} else {
			a.confirmThen("Copy your data to:\n"+path+"\n\nHangar will use that folder from the next restart. Your current data folder is left untouched until you choose to delete it.",
				"Move data", false, func() { d.moveData(a, path) })
		}
	}
	if d.delPrev.Clicked(gtx) {
		prev := str(v["previous_data_dir"])
		a.confirmThen("Delete the old data folder?\n\n"+prev+"\n\nThis can't be undone. Do it only once you've checked Hangar works from the new folder.",
			"Delete", true, func() {
				if _, err := deletePreviousDataDir(); err != nil {
					d.say(errorText(err), true)
				} else {
					d.say("Deleted the old data folder.", false)
				}
				a.applySettings(settingsValues())
			})
	}
	for name, c := range d.restore {
		if c.Clicked(gtx) {
			d.confirmRestore(a, name)
		}
	}
	for _, n := range []struct {
		ed  *widget.Editor
		key string
		lo  int
		hi  int
	}{{&d.port, "port", 1024, 65535}, {&d.daily, "backups_daily", 1, 365}, {&d.snapshots, "backups_snapshots", 1, 100}} {
		for {
			ev, ok := n.ed.Update(gtx)
			if !ok {
				break
			}
			val, err := strconv.Atoi(strings.TrimSpace(n.ed.Text()))
			valid := err == nil && val >= n.lo && val <= n.hi
			switch ev.(type) {
			case widget.ChangeEvent:
				if valid && val != v[n.key] {
					d.save(a, map[string]any{n.key: val})
				}
			case widget.SubmitEvent:
				if !valid {
					d.say(fmt.Sprintf("%s must be a whole number from %d to %d", n.key, n.lo, n.hi), true)
				}
			}
		}
	}

	choice := func(dd *Dropdown, key string, opts []option) layout.Widget {
		return func(gtx C) D {
			gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(190), gtx.Dp(190)
			_, isEnv := env[key]
			dims, val, changed := l.dropdown(gtx, dd, opts, str(v[key]), false, isEnv)
			if changed {
				d.save(a, map[string]any{key: val})
			}
			return dims
		}
	}
	check := func(key string) layout.Widget {
		return func(gtx C) D {
			b := a.boolean("setting|" + key)
			if b.Update(gtx) {
				d.save(a, map[string]any{key: b.Value})
			}
			b.Value = v[key] == true
			return l.checkbox(gtx, b, "", false)
		}
	}
	number := func(e *widget.Editor, key string) layout.Widget {
		return func(gtx C) D {
			gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(190), gtx.Dp(190)
			if _, isEnv := env[key]; isEnv {
				gtx = gtx.Disabled()
			}
			return l.field(gtx, e, fieldOpts{})
		}
	}
	setting := func(label, hint string, control layout.Widget) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return column(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
						return hrow(gtx,
							layout.Flexed(1, func(gtx C) D {
								return column(gtx,
									layout.Rigid(func(gtx C) D { return l.plain(gtx, label) }),
									layout.Rigid(func(gtx C) D {
										if hint == "" {
											return D{}
										}
										return l.text(gtx, hint, textStyle{size: 12, color: l.p.text2})
									}),
								)
							}),
							hspace(14),
							layout.Rigid(control),
						)
					})
				}),
				layout.Rigid(l.divider),
			)
		})
	}
	block := func(w layout.Widget) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return column(gtx,
				layout.Rigid(func(gtx C) D { return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, w) }),
				layout.Rigid(l.divider),
			)
		})
	}
	section := func(title string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 22, Bottom: 6}.Layout(gtx, func(gtx C) D {
				return l.text(gtx, strings.ToUpper(title), textStyle{size: 11, weight: font.SemiBold, color: l.p.text3})
			})
		})
	}

	browserOpts := []option{{"auto", "Automatic"}}
	browsers := installedBrowsers()
	for _, b := range browsers {
		browserOpts = append(browserOpts, option{b.Path, b.Name})
	}
	browserHint := "Chrome, Edge or Brave."
	if _, isEnv := env["rsi_browser"]; isEnv {
		browserHint = "Chosen by the HANGAR_BROWSER environment variable."
	} else if len(browsers) == 0 {
		browserHint = "No Chrome, Edge or Brave was found, so import with the bookmark instead."
	}
	portHint := "Where Hangar listens for the RSI bookmark on this computer. Changing it needs a restart."
	if _, isEnv := env["port"]; isEnv {
		portHint = "Chosen by the SHIPS_PORT environment variable."
	}
	forced := a.privacyHidden()
	hidden, _ := v["hidden_columns"].([]string)
	dataText := dataDir
	if want := str(v["data_dir"]); d.restartNeeded(a) && want != "" && want != dataDir {
		dataText += "\nFrom the next start: " + want
	}

	children := []layout.FlexChild{
		layout.Rigid(func(gtx C) D { return l.heading(gtx, "Settings") }),
	}
	if d.restartNeeded(a) {
		children = append(children, layout.Rigid(func(gtx C) D {
			return background(gtx, l.p.noneBg, rgba(0, 0, 0, 0), 8, pad(8, 12, 8, 12, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return hrow(gtx,
					layout.Flexed(1, func(gtx C) D {
						return l.text(gtx, "Some changes only take effect after a restart.", textStyle{color: l.p.text2})
					}),
					layout.Rigid(func(gtx C) D {
						return l.button(gtx, &d.restart, "Restart Hangar now", btnOpts{kind: btnPrimary, small: true})
					}),
				)
			}))
		}))
	}
	children = append(children,
		section("Appearance"),
		setting("Theme", "System follows your computer's light or dark setting.", choice(&d.theme, "theme",
			[]option{{"system", "System"}, {"light", "Light"}, {"dark", "Dark"}})),
		setting("Font size", "", choice(&d.fontSize, "font_size", []option{{"small", "Small"}, {"default", "Default"}, {"large", "Large"}})),
		setting("Thumbnail size", "", choice(&d.thumb, "thumb_size",
			[]option{{"small", "Small (54×33)"}, {"medium", "Medium (108×66)"}, {"large", "Large (216×133)"}})),
		setting("Row density", "", choice(&d.density, "density", []option{{"comfortable", "Comfortable"}, {"compact", "Compact"}})),
		setting("Date format", "", choice(&d.dateFmt, "date_format", []option{{"iso", "2026-10-14"}, {"long", "Oct 14, 2026"}})),
		block(func(gtx C) D {
			return column(gtx,
				layout.Rigid(func(gtx C) D { return l.plain(gtx, "Columns shown") }),
				layout.Rigid(func(gtx C) D {
					return l.text(gtx, "Untick the columns you don't use. Name always stays.", textStyle{size: 12, color: l.p.text2})
				}),
				vspace(8),
				layout.Rigid(func(gtx C) D {
					cols := []*Column{}
					for i := range columns {
						if columns[i].Get != nil && columns[i].Key != "name" {
							cols = append(cols, &columns[i])
						}
					}
					per := max(1, gtx.Constraints.Max.X/gtx.Dp(165))
					rows := []layout.FlexChild{}
					for start := 0; start < len(cols); start += per {
						start := start
						rows = append(rows, layout.Rigid(func(gtx C) D {
							cells := []layout.FlexChild{}
							for i := start; i < start+per && i < len(cols); i++ {
								c := cols[i]
								cells = append(cells, layout.Flexed(1, func(gtx C) D {
									off := contains(forced, c.Key)
									b := a.boolean("col|" + c.Key)
									if b.Update(gtx) {
										set := []string{}
										for _, k := range hidden {
											if k != c.Key {
												set = append(set, k)
											}
										}
										if !b.Value {
											set = append(set, c.Key)
										}
										d.save(a, map[string]any{"hidden_columns": toAny(set)})
									}
									b.Value = !off && !contains(hidden, c.Key)
									return layout.Inset{Top: 2, Bottom: 2}.Layout(gtx, func(gtx C) D { return l.checkbox(gtx, b, c.Label, off) })
								}))
							}
							for i := len(cells); i < per; i++ {
								cells = append(cells, layout.Flexed(1, func(gtx C) D { return D{} }))
							}
							return layout.Flex{}.Layout(gtx, cells...)
						}))
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
				}),
			)
		}),
		section("Privacy"),
		setting("Hide dollar amounts", "Removes the Purchase and Value columns, and hides the amounts shown elsewhere (package cards, editors, totals). Good for streaming.",
			check("hide_amounts")),
		setting("Hide pledge IDs", "Removes the Pledge ID and Ship ID columns, and hides the pledge numbers shown elsewhere. Gift recipients are always hidden until you click them.",
			check("hide_ids")),
		section("Startup"),
		setting("Start on", "", choice(&d.startOn, "start_view", []option{{"items", "Items"}, {"packages", "Packages"}})),
		setting("Port", portHint, number(&d.port, "port")),
		section("RSI import"),
		setting("Close Hangar's RSI window when an import finishes", "Your sign-in is kept.", check("rsi_close_after_import")),
		setting("Browser for the RSI window", browserHint, func(gtx C) D {
			gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(190), gtx.Dp(190)
			_, isEnv := env["rsi_browser"]
			sel := str(v["rsi_browser"])
			found := false
			for _, o := range browserOpts {
				found = found || o.value == sel
			}
			if !found {
				sel = "auto"
			}
			dims, val, changed := l.dropdown(gtx, &d.browse, browserOpts, sel, false, isEnv || len(browsers) == 0)
			if changed {
				d.save(a, map[string]any{"rsi_browser": val})
			}
			return dims
		}),
		setting("Download pictures during an import", "Turn off to import quickly or save disk space; items then show a symbol.", check("download_pictures")),
		section("Data and backups"),
		block(func(gtx C) D {
			_, envData := env["data_dir"]
			if envData {
				gtx = gtx.Disabled()
			}
			return column(gtx,
				layout.Rigid(func(gtx C) D { return l.plain(gtx, "Data folder") }),
				layout.Rigid(func(gtx C) D {
					return l.text(gtx, "Your database, pictures, backups and RSI sign-in live here.", textStyle{size: 12, color: l.p.text2})
				}),
				vspace(8),
				layout.Rigid(func(gtx C) D { return l.text(gtx, dataText, textStyle{size: 12, color: l.p.text2, mono: true}) }),
				vspace(8),
				layout.Rigid(func(gtx C) D {
					return hrow(gtx,
						layout.Flexed(1, func(gtx C) D {
							return l.field(gtx, &d.movePath, fieldOpts{hint: "Move my data to this folder, e.g. ~/Documents/Hangar-data"})
						}),
						hspace(8),
						layout.Rigid(func(gtx C) D { return l.button(gtx, &d.pick, "Choose folder…", btnOpts{disabled: d.busy}) }),
						hspace(8),
						layout.Rigid(func(gtx C) D { return l.button(gtx, &d.move, "Move data…", btnOpts{disabled: d.busy}) }),
					)
				}),
				layout.Rigid(func(gtx C) D {
					if !envData {
						return D{}
					}
					return l.text(gtx, "The SHIPS_DATA environment variable chooses this folder, so it can't be moved from here.", textStyle{size: 12, color: l.p.text2})
				}),
				layout.Rigid(func(gtx C) D {
					prev := str(v["previous_data_dir"])
					if prev == "" {
						return D{}
					}
					return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
						return hrow(gtx,
							layout.Flexed(1, func(gtx C) D {
								return l.text(gtx, "Old data folder, kept until you delete it: "+prev, textStyle{size: 12, color: l.p.text2, mono: true})
							}),
							hspace(8),
							layout.Rigid(func(gtx C) D { return l.button(gtx, &d.delPrev, "Delete the old folder…", btnOpts{kind: btnDanger}) }),
						)
					})
				}),
			)
		}),
		setting("Daily backups to keep", "Hangar saves one each day it starts.", number(&d.daily, "backups_daily")),
		setting("Safety copies to keep", "Saved before every RSI import and every restore.", number(&d.snapshots, "backups_snapshots")),
		block(func(gtx C) D { return d.backupTable(gtx, a) }),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
				return l.text(gtx, "Settings file: "+settingsPath(), textStyle{size: 12, color: l.p.text2})
			})
		}),
		section("About"),
		layout.Rigid(func(gtx C) D {
			return column(gtx,
				layout.Rigid(func(gtx C) D { return l.plain(gtx, "Hangar "+version) }),
				vspace(4),
				layout.Rigid(func(gtx C) D { return l.text(gtx, disclaimer, textStyle{size: 12, color: l.p.text2}) }),
			)
		}),
		layout.Rigid(func(gtx C) D {
			c := l.p.text2
			if d.msgErr {
				c = l.p.danger
			}
			return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D { return l.text(gtx, d.msg, textStyle{color: c}) })
		}),
		vspace(18),
		layout.Rigid(func(gtx C) D {
			return footer(gtx, nil, func(gtx C) D { return l.button(gtx, &d.done, "Done", btnOpts{}) })
		}),
	)
	_, outside := l.sheet(gtx, &d.sheet, 780, func(gtx C) D { return column(gtx, children...) })
	if outside && a.confirm == nil {
		a.settingsDlg = nil
	}
}

// disclaimer is shown in Settings → About and on the welcome screen.
const disclaimer = "Hangar is an unofficial, fan-made tool. It is not affiliated with, endorsed or sponsored by Cloud Imperium Games " +
	"or Roberts Space Industries. Star Citizen®, Roberts Space Industries® and Cloud Imperium® are trademarks of Cloud Imperium " +
	"Rights LLC and Cloud Imperium Rights Ltd. Hangar only reads your own account pages, in your own signed-in browser, and keeps " +
	"everything on this computer. It comes with no warranty: keep backups, and use it at your own risk."

func toAny(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

func fmtSize(n int64) string {
	if n < 1024*1024 {
		return fmt.Sprintf("%d KB", max(1, int((n+512)/1024)))
	}
	return fmt.Sprintf("%.1f MB", float64(n)/1048576)
}

func (a *App) fmtWhen(iso string) string {
	return a.disp(strings.Replace(iso, "T", " ", 1))
}

func (d *settingsDialog) backupTable(gtx C, a *App) D {
	l := a.look
	head := []string{"Kind", "When", "Holds", "Size", ""}
	widths := []float32{0.26, 0.24, 0.26, 0.1, 0.14}
	rowOf := func(cellsText []string, header bool, restore *widget.Clickable) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return column(gtx,
				layout.Rigid(func(gtx C) D {
					children := []layout.FlexChild{}
					for i, s := range cellsText {
						i, s := i, s
						children = append(children, layout.Flexed(widths[i], func(gtx C) D {
							return layout.Inset{Top: 5, Bottom: 5, Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
								if i == 4 && restore != nil {
									return l.button(gtx, restore, "Restore…", btnOpts{small: true})
								}
								st := textStyle{size: 12}
								if header {
									st = textStyle{size: 12, weight: font.SemiBold, color: l.p.text2}
								} else if i == 3 || s == "can't be read" {
									st.color = l.p.text3
								}
								return l.text(gtx, s, st)
							})
						}))
					}
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
				}),
				layout.Rigid(l.divider),
			)
		})
	}
	rows := []layout.FlexChild{
		layout.Rigid(func(gtx C) D { return l.plain(gtx, "Backups") }),
		vspace(8),
		layout.Rigid(func(gtx C) D {
			return hrow(gtx,
				layout.Rigid(func(gtx C) D { return l.button(gtx, &d.backupNow, "Back up now", btnOpts{}) }),
				hspace(8),
				layout.Rigid(func(gtx C) D { return l.button(gtx, &d.openBackups, "Open backup folder", btnOpts{}) }),
			)
		}),
		vspace(8),
		rowOf(head, true, nil),
	}
	if len(d.backups) == 0 {
		rows = append(rows, layout.Rigid(func(gtx C) D {
			return layout.UniformInset(8).Layout(gtx, func(gtx C) D {
				return l.text(gtx, "No backups yet. One is saved each day Hangar starts.", textStyle{size: 12, color: l.p.text3})
			})
		}))
	}
	for _, b := range d.backups {
		name := str(b["name"])
		holds := "can't be read"
		var restore *widget.Clickable
		if b["packages"] != nil {
			holds = fmt.Sprintf("%v packages, %v items", b["packages"], b["items"])
			restore = d.restore[name]
			if restore == nil {
				restore = new(widget.Clickable)
				d.restore[name] = restore
			}
		}
		size, _ := b["size"].(int64)
		rows = append(rows, rowOf([]string{str(b["label"]), a.fmtWhen(str(b["created"])), holds, fmtSize(size), ""}, false, restore))
	}
	rows = append(rows, vspace(6), layout.Rigid(func(gtx C) D {
		return l.text(gtx, "Pictures are not part of a backup. Back up the whole data folder to keep those too.", textStyle{size: 12, color: l.p.text2})
	}))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

func (d *settingsDialog) confirmRestore(a *App, name string) {
	var b map[string]any
	for _, x := range d.backups {
		if str(x["name"]) == name {
			b = x
		}
	}
	if b == nil {
		return
	}
	msg := fmt.Sprintf("Restore this backup?\n\n%s, %s\n%v packages, %v items\n\nHangar switches to exactly what is in the backup. "+
		"What you have now is saved first as a \"Before a restore\" backup, so you can undo this. Pictures aren't part of a backup and stay as they are.",
		str(b["label"]), a.fmtWhen(str(b["created"])), b["packages"], b["items"])
	a.confirmThen(msg, "Restore", false, func() {
		dbMu.Lock()
		r, err := restoreBackup(name)
		dbMu.Unlock()
		if err != nil {
			d.say(errorText(err), true)
			return
		}
		thumbs.forget()
		a.reload()
		d.refreshBackups(a)
		d.say(fmt.Sprintf("Restored %v packages and %v items. Your previous data was saved as %v.", r["packages"], r["items"], r["safety_copy"]), false)
	})
}

func (d *settingsDialog) moveData(a *App, path string) {
	d.busy = true
	d.say("Copying…", false)
	go func() {
		window.close() // its saved sign-in lives in the data folder and is copied too
		dbMu.Lock()
		r, err := moveDataDir(path)
		dbMu.Unlock()
		a.post(func() {
			d.busy = false
			a.applySettings(settingsValues())
			if err != nil {
				d.say(errorText(err), true)
				return
			}
			d.movePath.SetText("")
			if r["mode"] == "switch" {
				d.say("That folder already holds Hangar data, so Hangar will use it after the restart. Nothing was copied.", false)
			} else {
				d.say(fmt.Sprintf("Copied. Hangar will use %v after the restart.", r["target"]), false)
			}
		})
	}()
}

// useExistingFolder: on the welcome screen, point Hangar at a data folder made before (by this or the earlier Hangar).
func (a *App) useExistingFolder() {
	go func() {
		path, err := pickFolder()
		if err != nil || path == "" {
			if err != nil {
				a.post(func() { a.toast(errorText(err)) })
			}
			return
		}
		if !fileExists(path+string(osSep)+"ships.db") && fileExists(path+string(osSep)+"data"+string(osSep)+"ships.db") {
			path = path + string(osSep) + "data" // the Hangar program folder was chosen: its data folder is inside
		}
		if !fileExists(path + string(osSep) + "ships.db") {
			a.post(func() { a.toast("That folder has no Hangar database (ships.db) in it.") })
			return
		}
		dbMu.Lock()
		_, err = moveDataDir(path)
		dbMu.Unlock()
		a.post(func() {
			if err != nil {
				a.toast(errorText(err))
				return
			}
			a.confirmThen("Hangar will use the data in\n"+path+"\nfrom now on. It restarts to open it.", "Restart now", false, a.restartApp)
		})
	}()
}

var _ = text.Start
var _ unit.Dp
