package main

// Pictures for the README, drawn off-screen. With HANGAR_DOCS_SOURCE set to a Hangar data folder they show that real
// hangar, from a scrubbed copy: the folder itself is only read, and the copy loses every price, value, date, pledge ID,
// ship ID, upgrade detail, gift recipient, note and custom ship name before anything is drawn. Without it, they show a
// made-up sample hangar. Run with HANGAR_DOCS_DIR set to the folder for the PNG files:
//
//	HANGAR_DOCS_DIR=docs HANGAR_DOCS_SOURCE=~/Library/Application\ Support/Hangar/data go test -run TestDocsScreenshots .

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"golang.org/x/image/vector"
)

type sampleItem struct {
	name, typ, maker, purpose, insurance, crew, scu, flight string
	picture                                                 int // 0 = none
}

type samplePackage struct {
	name, date, purchase, value, pledgeID, status string
	items                                         []sampleItem
}

var samplePackages = []samplePackage{
	{"Starter Pack - Wayfarer Explorer", "2026-03-14", "$45.00 USD", "$45.00 USD", "10000007", "owned", []sampleItem{
		{"Wayfarer Explorer", "Ship", "Example Spaceworks", "Exploration", "Lifetime", "1-2", "8", "Yes", 1},
		{"Wayfarer - Nebula Paint", "Skin", "", "Paint", "", "", "", "", 2},
		{"Self-Land Hangar", "Hangar", "", "", "", "", "", "", 0},
	}},
	{"Standalone Ship - Longhaul Freighter", "2025-11-02", "$140.00 USD", "$180.00 USD", "10000006", "owned", []sampleItem{
		{"Longhaul Freighter", "Ship", "Atlas Freight Works", "Medium Freight", "120 months", "3", "96", "Yes", 3},
	}},
	{"Paint Pack - Aurora", "2025-08-19", "$12.00 USD", "$12.00 USD", "10000005", "owned", []sampleItem{
		{"Longhaul Freighter - Aurora Paint", "Skin", "", "Paint", "", "", "", "", 4},
		{"Wayfarer - Aurora Paint", "Skin", "", "Paint", "", "", "", "", 5},
	}},
	{"Gear Bundle - Deep Space Kit", "2025-02-07", "$30.00 USD", "$30.00 USD", "10000004", "owned", []sampleItem{
		{"Pathfinder Armor Set", "FPS Equipment", "Kestrel Outfitters", "Armor", "", "", "", "", 6},
		{"Survey Scanner", "FPS Equipment", "Kestrel Outfitters", "Tools", "", "", "", "", 7},
		{"Digital Download Manual", "Game Components", "", "", "", "", "", "", 0},
	}},
	{"Standalone Ship - Swift Racer", "2024-06-21", "$75.00 USD", "$75.00 USD", "10000003", "owned", []sampleItem{
		{"Swift Racer", "Ship", "Velocity Dynamics", "Racing", "6 months", "1", "", "In Concept", 8},
	}},
	{"Flair - Desk Globe", "2023-12-01", "$5.00 USD", "$5.00 USD", "10000002", "owned", []sampleItem{
		{"Desk Globe", "Hangar decoration", "", "Flair", "", "", "", "", 9},
	}},
	{"Standalone Ship - Old Hauler", "2022-05-30", "$50.00 USD", "$50.00 USD", "10000001", "melted", []sampleItem{
		{"Old Hauler", "Ship", "Atlas Freight Works", "Light Freight", "Lifetime", "1", "32", "Yes", 10},
	}},
}

// samplePicture draws an invented "screenshot": a coloured sky, a planet and a small ship.
func samplePicture(seed int) []byte {
	const w, h = 480, 300
	rng := rand.New(rand.NewSource(int64(seed)))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	top := color.RGBA{uint8(10 + rng.Intn(40)), uint8(20 + rng.Intn(60)), uint8(60 + rng.Intn(120)), 255}
	bottom := color.RGBA{uint8(rng.Intn(30)), uint8(rng.Intn(30)), uint8(20 + rng.Intn(40)), 255}
	for y := 0; y < h; y++ {
		t := float64(y) / h
		c := color.RGBA{uint8(float64(top.R)*(1-t) + float64(bottom.R)*t), uint8(float64(top.G)*(1-t) + float64(bottom.G)*t),
			uint8(float64(top.B)*(1-t) + float64(bottom.B)*t), 255}
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	for i := 0; i < 40; i++ {
		img.SetRGBA(rng.Intn(w), rng.Intn(h), color.RGBA{255, 255, 255, 255})
	}
	fill := func(build func(r *vector.Rasterizer), c color.RGBA) {
		r := vector.NewRasterizer(w, h)
		build(r)
		r.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{})
	}
	circle := func(r *vector.Rasterizer, cx, cy, rad float32) {
		const k = 0.5523
		r.MoveTo(cx+rad, cy)
		r.CubeTo(cx+rad, cy+rad*k, cx+rad*k, cy+rad, cx, cy+rad)
		r.CubeTo(cx-rad*k, cy+rad, cx-rad, cy+rad*k, cx-rad, cy)
		r.CubeTo(cx-rad, cy-rad*k, cx-rad*k, cy-rad, cx, cy-rad)
		r.CubeTo(cx+rad*k, cy-rad, cx+rad, cy-rad*k, cx+rad, cy)
		r.ClosePath()
	}
	planet := color.RGBA{uint8(80 + rng.Intn(170)), uint8(60 + rng.Intn(150)), uint8(60 + rng.Intn(190)), 255}
	px, py, pr := float32(60+rng.Intn(120)), float32(200+rng.Intn(60)), float32(90+rng.Intn(60))
	fill(func(r *vector.Rasterizer) { circle(r, px, py, pr) }, planet)
	// the ship, flying up and to the right
	sx, sy := float32(260+rng.Intn(80)), float32(90+rng.Intn(60))
	fill(func(r *vector.Rasterizer) {
		r.MoveTo(sx+90, sy)
		r.LineTo(sx+10, sy+40)
		r.LineTo(sx-30, sy+80)
		r.LineTo(sx+20, sy+62)
		r.LineTo(sx+30, sy+90)
		r.LineTo(sx+48, sy+50)
		r.ClosePath()
	}, color.RGBA{235, 240, 250, 255})
	fill(func(r *vector.Rasterizer) { circle(r, sx-24, sy+86, 9) }, color.RGBA{255, 159, 10, 255})
	var out bytes.Buffer
	jpeg.Encode(&out, img, &jpeg.Options{Quality: 88})
	return out.Bytes()
}

func TestDocsScreenshots(t *testing.T) {
	out := os.Getenv("HANGAR_DOCS_DIR")
	if out == "" {
		t.Skip("set HANGAR_DOCS_DIR")
	}
	home := t.TempDir()
	baseDir = home
	setDataDir(filepath.Join(home, "data"))
	setPort(configuredPort())
	if source := os.Getenv("HANGAR_DOCS_SOURCE"); source != "" {
		copyScrubbed(t, source)
	} else {
		makeSample(t)
	}
	// whatever is left that looks like money or an ID is hidden too, and the date and money columns are off
	v := settingsValues()
	v["hide_amounts"], v["hide_ids"] = true, true
	v["hidden_columns"] = []any{"pledge_date", "purchase_value", "value", "pledge_id", "membership_id"}
	v["thumb_size"], v["font_size"], v["density"] = "medium", "default", "comfortable"
	settingsMu.Lock()
	writeSettings(v)
	settingsMu.Unlock()

	a := newApp(new(app.Window))
	a.reload()
	shoot := func(name, theme string, setup func()) {
		v := settingsValues()
		v["theme"] = theme
		a.applySettings(v)
		setup()
		const w, h, scale = 1280, 720, 2
		win, err := headless.NewWindow(w*scale, h*scale)
		if err != nil {
			t.Fatal(err)
		}
		defer win.Release()
		for i := 0; i < 5; i++ {
			var ops op.Ops
			gtx := layout.Context{Ops: &ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale},
				Constraints: layout.Exact(image.Pt(w*scale, h*scale))}
			a.frame(gtx)
			if err := win.Frame(&ops); err != nil {
				t.Fatal(err)
			}
			time.Sleep(400 * time.Millisecond)
		}
		img := image.NewRGBA(image.Rectangle{Max: image.Pt(w*scale, h*scale)})
		if err := win.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		small := image.NewRGBA(image.Rect(0, 0, w*3/2, h*3/2)) // 1.5× keeps the files small and still sharp
		draw.Draw(small, small.Bounds(), cover(img, small.Bounds().Size()), image.Point{}, draw.Src)
		f, err := os.Create(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		png.Encode(f, small)
		f.Close()
	}
	os.MkdirAll(out, 0o755)
	shoot("screenshot-items.png", "light", func() { a.view.Mode = "items" })
	query := "freighter"
	if os.Getenv("HANGAR_DOCS_SOURCE") != "" {
		query = "constellation"
	}
	shoot("screenshot-search.png", "light", func() { a.view.Query = query; a.search.SetText(query) })
	shoot("screenshot-packages.png", "dark", func() { a.view.Query = ""; a.search.SetText(""); a.view.Mode = "packages" })
	shoot("screenshot-import.png", "light", func() {
		a.view.Mode = "items"
		a.rsiDlg = &rsiDialog{stop: make(chan struct{}), checked: true, browser: "Google Chrome", open: true, onRSI: true, signedIn: true}
	})
	a.rsiDlg = nil
}

// copyScrubbed copies a real hangar (database and pictures) and removes from the copy everything personal or financial.
func copyScrubbed(t *testing.T, source string) {
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyDatabaseFile(filepath.Join(source, "ships.db"), dbPath); err != nil {
		t.Fatal(err)
	}
	conn, err := connect()
	if err != nil {
		t.Fatal(err)
	}
	store = conn
	for _, q := range []string{
		"UPDATE packages SET pledge_date = NULL, value = NULL, purchase_value = NULL, rsi_pledge_id = NULL, status_date = NULL, " +
			"status_note = NULL, gifted_to = NULL, notes = NULL, gift_pending = 0",
		"UPDATE items SET membership_id = NULL, custom_name = NULL, notes = NULL",
		"DELETE FROM meta",
	} {
		if _, err := store.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	// upgrades keep only which ship became which: no dates, prices, values or upgrade IDs
	rows := mustRows(store, "SELECT id, upgrade_history FROM items WHERE upgrade_history IS NOT NULL")
	for _, r := range rows {
		steps := []map[string]string{}
		for _, h := range parseHistory(txt(r["upgrade_history"])) {
			steps = append(steps, map[string]string{"from": h.From, "to": h.To})
		}
		mustExec(store, "UPDATE items SET upgrade_history = ? WHERE id = ?", toJSON(steps), r["id"])
	}
	for _, r := range mustRows(store, "SELECT image FROM items WHERE image IS NOT NULL") {
		name := filepath.Base(txt(r["image"]))
		if st, err := os.Stat(filepath.Join(source, "images", name)); err == nil && st.Mode().IsRegular() {
			copyFile(filepath.Join(source, "images", name), filepath.Join(imageDir, name), 0o644)
		}
	}
	// nothing that was wiped may survive anywhere in the copy
	left := queryInt(store, "SELECT COUNT(*) FROM packages WHERE pledge_date IS NOT NULL OR value IS NOT NULL OR purchase_value IS NOT NULL "+
		"OR rsi_pledge_id IS NOT NULL OR gifted_to IS NOT NULL OR status_note IS NOT NULL OR notes IS NOT NULL")
	left += queryInt(store, "SELECT COUNT(*) FROM items WHERE membership_id IS NOT NULL OR custom_name IS NOT NULL OR notes IS NOT NULL "+
		"OR upgrade_history LIKE '%date%' OR upgrade_history LIKE '%price%' OR upgrade_history LIKE '%upgrade_id%'")
	if left != 0 {
		t.Fatalf("%d rows still hold personal or financial details", left)
	}
}

func makeSample(t *testing.T) {
	conn, err := connect()
	if err != nil {
		t.Fatal(err)
	}
	store = conn
	for _, p := range samplePackages {
		pkg, err := createPackage(store, map[string]any{"name": p.name, "pledge_date": p.date, "purchase_value": p.purchase,
			"value": p.value, "rsi_pledge_id": p.pledgeID, "status": p.status})
		if err != nil {
			t.Fatal(err)
		}
		if p.status == "melted" {
			updatePackage(store, num(pkg["id"]), map[string]any{"status_date": "2024-01-15", "status_note": "Store credit $50.00 USD"})
		}
		for _, it := range p.items {
			row, err := createItem(store, map[string]any{"name": it.name, "type": it.typ, "manufacturer": it.maker, "purpose": it.purpose,
				"insurance": it.insurance, "crew": it.crew, "storage_scu": it.scu, "flight_ready": it.flight, "package_id": float64(num(pkg["id"]))})
			if err != nil {
				t.Fatal(err)
			}
			if it.picture > 0 {
				if _, err := saveItemImage(num(row["id"]), samplePicture(it.picture), "image/jpeg"); err != nil {
					t.Fatal(err)
				}
			}
			if it.name == "Longhaul Freighter" {
				store.Exec("UPDATE items SET upgrade_history = ? WHERE id = ?",
					`[{"from":"Courier Mk I","to":"Longhaul Freighter","date":"2026-01-10T18:00","upgrade_id":"20000001","value":"$180.00 USD","price":"$40.00 USD"}]`, num(row["id"]))
			}
		}
	}

}
