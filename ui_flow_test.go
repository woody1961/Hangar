package main

// The window's actions, run without clicking: save a new package and item through the dialogs, import pasted
// bookmark data, receive a bookmark import, and export. Uses the same copy as TestSnapshots (HANGAR_SNAPSHOT_DATA).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/app"
)

func TestWindowActions(t *testing.T) {
	data := os.Getenv("HANGAR_SNAPSHOT_DATA")
	if data == "" {
		t.Skip("set HANGAR_SNAPSHOT_DATA")
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
	before := len(a.hangar.Packages)

	// New Package → Save opens the item editor for its first item
	a.openPackage(nil)
	a.pkgDlg.name.SetText("Test Pack From The Window")
	a.pkgDlg.savePackage(a)
	if a.pkgDlg != nil || a.itemDlg == nil || len(a.hangar.Packages) != before+1 {
		t.Fatalf("package not saved: dialog %v, %d packages", a.pkgDlg != nil, len(a.hangar.Packages))
	}
	a.itemDlg.name.SetText("Window Test Ship")
	a.itemDlg.typ.SetText("Ship")
	a.itemDlg.scu.SetText("lots")
	a.itemDlg.saveItem(a)
	if !strings.Contains(a.itemDlg.errText, "Storage must be a number") {
		t.Fatalf("bad storage accepted: %q", a.itemDlg.errText)
	}
	a.itemDlg.scu.SetText("96")
	a.itemDlg.saveItem(a)
	a.runPending()
	var found *Item
	for _, it := range a.hangar.Items {
		if it.Name == "Window Test Ship" {
			found = it
		}
	}
	if a.itemDlg != nil || found == nil || found.StorageSCU != "96" || a.hangar.pkgOf(found).Name != "Test Pack From The Window" {
		t.Fatalf("item not saved: %+v", found)
	}

	// search finds it, and the banner says it is owned
	a.view.Query = "window test"
	if msg, owned := a.hangar.ownershipText(&a.view); !owned || !strings.Contains(msg, "You already own") {
		t.Fatalf("banner: %q", msg)
	}
	a.view.Query = ""

	// pasted bookmark data
	payload := `{"complete": false, "pledges": [{"id": "99000001", "name": "Pasted Pack", "value": "$10.00 USD", "created": "Created: May 1, 2026",
	  "items": [{"title": "Pasted Ship", "kind": "Ship", "manufacturer": "Drake Interplanetary", "code": "DRAK"}]}], "upgrades": [], "events": []}`
	a.openRSI()
	unique := fmt.Sprint(time.Now().Unix() % 100000000) // a pledge this copy hasn't seen in an earlier run
	payload = strings.ReplaceAll(payload, "99000001", unique)
	a.rsiDlg.paste.SetText(payload)
	a.rsiDlg.importPasted(a)
	deadline := time.Now().Add(10 * time.Second)
	for a.rsiDlg.pasting && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		a.runPending()
	}
	if a.rsiDlg.resultErr || len(a.rsiDlg.result) == 0 || !strings.HasPrefix(a.rsiDlg.result[0].text, "Imported 1 pledges: 1 new") {
		t.Fatalf("paste import: %+v", a.rsiDlg.result)
	}
	a.closeRSI()

	// the bookmark sends to the receiver from RSI's site
	got := make(chan *importSummary, 1)
	srv := httptest.NewServer(receiverHandler(func(s *importSummary) { got <- s }))
	defer srv.Close()
	body := strings.Replace(payload, unique, unique+"7", 1)
	req, _ := http.NewRequest("POST", srv.URL+"/api/import/rsi", strings.NewReader(body))
	req.Host = "localhost:" + itoa(port)
	req.Header.Set("Origin", rsiOrigin)
	req.Header.Set("X-Ships", "1")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Access-Control-Allow-Origin") != rsiOrigin {
		t.Fatalf("bookmark import: %v %v", err, resp)
	}
	if s := <-got; s.Created != 0 || s.Matched != 0 {
		// same name as the pasted pack but a different pledge id: a new package
		if s.Created != 1 {
			t.Fatalf("bookmark summary: %+v", s)
		}
	}
	// another site may not send
	req, _ = http.NewRequest("POST", srv.URL+"/api/import/rsi", strings.NewReader(body))
	req.Host = "localhost:" + itoa(port)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("X-Ships", "1")
	if resp, _ := http.DefaultClient.Do(req); resp == nil || resp.StatusCode != 403 {
		t.Fatalf("a foreign site was let in: %v", resp)
	}

	// exports
	var csvOut bytes.Buffer
	if err := writeCSV(&csvOut); err != nil || !strings.Contains(csvOut.String(), "Window Test Ship") {
		t.Fatalf("csv: %v", err)
	}
	js, err := exportJSON()
	var parsed map[string]any
	if err != nil || json.Unmarshal(js, &parsed) != nil || parsed["items"] == nil {
		t.Fatalf("json: %v", err)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// TestPicturesAppearWithoutTheDialog: after an import the window shows the pictures once they are downloaded, even
// with the Import dialog closed, and a download that fails for a moment is tried again.
func TestPicturesAppearWithoutTheDialog(t *testing.T) {
	data := os.Getenv("HANGAR_SNAPSHOT_DATA")
	if data == "" {
		t.Skip("set HANGAR_SNAPSHOT_DATA")
	}
	baseDir = data
	setDataDir(filepath.Join(data, "data"))
	setPort(configuredPort())
	conn, err := connect()
	if err != nil {
		t.Fatal(err)
	}
	store = conn
	settingsMu.Lock()
	v := settingsValues()
	v["download_pictures"] = true
	writeSettings(v)
	settingsMu.Unlock()

	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	calls := 0
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(503) // busy the first time
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(buf.Bytes())
	}))
	defer cdn.Close()

	dbMu.Lock()
	it, err := createItem(store, map[string]any{"name": "Picture Test Item"})
	dbMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(new(app.Window))
	a.reload()
	go a.watchPictures()
	runImageJob([]imageTask{{num(it["id"]), cdn.URL + "/x.png"}})
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		a.runPending()
		for _, x := range a.hangar.Items {
			if x.ID == num(it["id"]) && x.Image != "" {
				if calls < 2 {
					t.Fatalf("no retry: %d calls", calls)
				}
				return
			}
		}
	}
	t.Fatalf("the picture never showed (%d downloads tried, status %v)", calls, imageStatus())
}
