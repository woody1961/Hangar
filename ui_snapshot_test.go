package main

// Pictures of the window, drawn off-screen, for checking the look without opening it: run with HANGAR_SNAPSHOT_DATA
// (a folder holding settings.json and data/, which gets modified, so use a copy) and HANGAR_SNAPSHOT_DIR (where the
// PNG files go).

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

func TestSnapshots(t *testing.T) {
	out, data := os.Getenv("HANGAR_SNAPSHOT_DIR"), os.Getenv("HANGAR_SNAPSHOT_DATA")
	if out == "" || data == "" {
		t.Skip("set HANGAR_SNAPSHOT_DIR and HANGAR_SNAPSHOT_DATA")
	}
	baseDir = data
	setDataDir(filepath.Join(data, "data"))
	setPort(configuredPort())
	conn, err := connect()
	if err != nil {
		t.Fatal(err)
	}
	store = conn
	a := newApp(new(app.Window))
	a.reload()
	os.MkdirAll(out, 0o755)

	const scale = 2
	render := func(name string, w, h int) {
		win, err := headless.NewWindow(w*scale, h*scale)
		if err != nil {
			t.Fatal(err)
		}
		defer win.Release()
		for i := 0; i < 4; i++ {
			var ops op.Ops
			gtx := layout.Context{Ops: &ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale},
				Constraints: layout.Exact(image.Pt(w*scale, h*scale))}
			a.frame(gtx)
			if err := win.Frame(&ops); err != nil {
				t.Fatal(err)
			}
			time.Sleep(600 * time.Millisecond) // pictures load in the background
		}
		img := image.NewRGBA(image.Rectangle{Max: image.Pt(w*scale, h*scale)})
		if err := win.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		f, _ := os.Create(filepath.Join(out, name+".png"))
		png.Encode(f, img)
		f.Close()
	}

	render("items", 1440, 900)
	a.view.Mode = "packages"
	render("packages", 1440, 900)
	a.view.Mode, a.view.Query = "items", "golem"
	a.search.SetText("golem")
	render("search", 1440, 900)
	a.view.Query = ""
	a.search.SetText("")
	if len(a.hangar.Items) > 0 {
		a.openItem(a.hangar.Items[0], 0)
		render("item-dialog", 1440, 900)
		a.itemDlg = nil
	}
	if len(a.hangar.Packages) > 0 {
		a.openPackage(a.hangar.Packages[0])
		render("package-dialog", 1440, 900)
		a.pkgDlg = nil
	}
	a.openSettings()
	render("settings", 1440, 900)
	a.settingsDlg = nil
	a.rsiDlg = &rsiDialog{stop: make(chan struct{}), checked: true, browser: "Brave Browser", showBookmark: true}
	render("rsi", 1440, 900)
	a.rsiDlg = nil
	v := settingsValues()
	v["theme"] = "light"
	a.applySettings(v)
	render("light", 1440, 900)
}
