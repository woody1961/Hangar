package main

// End to end: Hangar's own RSI window (a real headless Chrome/Edge/Brave with a throwaway profile) against a small
// stand-in for RSI's pledges pages and Hangar log. Runs only with HANGAR_LIVE_BROWSER=1, and only if a browser is found.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var mockPledges = []struct{ id, name, value, created, items string }{
	{"111", "Standalone Ship - Cutter", "$100.00 USD", "Created: October 14, 2026",
		`<div class="item"><div class="image" style="background-image:url('https://media.robertsspaceindustries.com/x/1.jpg')"></div><div class="text"><div class="title">Cutter</div><div class="kind">Ship</div><div class="liner">Drake Interplanetary (DRAK)</div></div></div>`},
	{"222", "Standalone Ship - HoverQuad", "$270.00 USD", "Created: February 10, 2022",
		`<div class="item"><div class="text"><div class="title">Apollo Medivac</div><div class="kind">Ship</div></div></div>`},
	{"333", "Package - Nomad Combo", "$96.00 USD", "Created: November 26, 2021",
		`<div class="item"><div class="text"><div class="title">Nomad</div><div class="kind">Ship</div></div></div>`},
}

var mockLog = []string{
	"Oct 14 2026, 9:08 pm - Standalone Ship - Cutter #111 - Created by Pilot - order #AAA, value: $100.00 USD",
	"Sep 26 2025, 5:28 pm - Standalone Ship - HoverQuad #222 - Upgrade applied: #900 Upgrade - HoverQuad to Apollo Medivac Warbond Edition, new value: $270.00 USD",
	"Jan 02 2020, 1:00 pm - Something gone #555 - Reclaimed by Pilot for $50.00 USD",
}

func mockRSI() *httptest.Server {
	mux := http.NewServeMux()
	signed := func(r *http.Request) bool { c, err := r.Cookie("mock"); return err == nil && c.Value == "1" }
	mux.HandleFunc("/signin", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "mock", Value: "1", Path: "/"})
		http.Redirect(w, r, "/en/account/pledges", http.StatusFound)
	})
	mux.HandleFunc("/en/account/pledges", func(w http.ResponseWriter, r *http.Request) {
		if !signed(r) {
			fmt.Fprint(w, "<html><body><h1>Sign in to RSI</h1></body></html>")
			return
		}
		page := 1
		fmt.Sscan(r.URL.Query().Get("page"), &page)
		var b strings.Builder
		b.WriteString(`<html><body><a class="js-pledge-log">Hangar log</a><ul class="list-items">`)
		for i, p := range mockPledges {
			if i/2+1 != page {
				continue
			}
			fmt.Fprintf(&b, `<li><input class="js-pledge-id" value="%s"><input class="js-pledge-name" value="%s"><input class="js-pledge-value" value="%s">`+
				`<div class="date-col"><label>%s</label></div><div class="items">%s</div></li>`, p.id, p.name, p.value, p.created, p.items)
		}
		b.WriteString(`</ul><div class="pager"><a href="/en/account/pledges?page=1">1</a><a href="/en/account/pledges?page=2">2</a></div></body></html>`)
		fmt.Fprint(w, b.String())
	})
	mux.HandleFunc("/api/account/pledgeLog", func(w http.ResponseWriter, r *http.Request) {
		if !signed(r) {
			w.WriteHeader(403)
			fmt.Fprint(w, "{}")
			return
		}
		var rendered strings.Builder
		for _, l := range mockLog {
			fmt.Fprintf(&rendered, `<div class="pledge-log-entry">%s</div>`, l)
		}
		json.NewEncoder(w).Encode(map[string]any{"success": 1, "data": map[string]any{"rendered": rendered.String(), "page": 1, "pagecount": 1}})
	})
	return httptest.NewServer(mux)
}

func TestRSIWindowEndToEnd(t *testing.T) {
	if os.Getenv("HANGAR_LIVE_BROWSER") != "1" {
		t.Skip("set HANGAR_LIVE_BROWSER=1 to run against a real browser")
	}
	dir := t.TempDir()
	t.Setenv("HANGAR_SETTINGS", filepath.Join(dir, "settings.json"))
	t.Setenv("HANGAR_BROWSER_HEADLESS", "1")
	setDataDir(filepath.Join(dir, "data"))
	if findBrowser() == nil {
		t.Skip("no Chrome, Edge or Brave installed")
	}
	rsi := mockRSI()
	defer rsi.Close()
	t.Setenv("HANGAR_RSI_URL", rsi.URL+"/en/account/pledges")

	w := &rsiWindow{}
	defer w.forget()
	name, err := w.open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Logf("opened %s on port %d", name, w.port)
	waitFor := func(what string, ok func(map[string]any) bool) map[string]any {
		deadline := time.Now().Add(20 * time.Second)
		for {
			s := w.status()
			if ok(s) {
				return s
			}
			if time.Now().After(deadline) {
				t.Fatalf("waiting for %s, status is %v", what, s)
			}
			time.Sleep(300 * time.Millisecond)
		}
	}
	s := waitFor("the RSI page", func(s map[string]any) bool { return s["on_rsi"] == true })
	if s["signed_in"] != false {
		t.Fatalf("signed in before signing in: %v", s)
	}
	if _, err := w.read(func(string, int, int) {}); err == nil {
		t.Fatal("read while signed out should refuse")
	}

	// the person signs in on RSI's own page (the stand-in sets its cookie and goes to the pledges page)
	c, err := dialCDP(w.target().WSURL, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.call("Page.navigate", map[string]any{"url": rsi.URL + "/signin"})
	c.close()
	waitFor("the sign-in", func(s map[string]any) bool { return s["signed_in"] == true })

	stages := map[string]bool{}
	data, err := w.read(func(stage string, cur, total int) { stages[stage] = true })
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	pledges, _ := data.Pledges.([]any)
	upgrades, _ := data.Upgrades.([]any)
	events, _ := data.Events.([]any)
	if len(pledges) != 3 || len(upgrades) != 1 || len(events) != 2 || pyText(data.LogNote) != "" {
		t.Fatalf("read %d pledges, %d upgrades, %d events, note %v", len(pledges), len(upgrades), len(events), data.LogNote)
	}
	if len(stages) == 0 {
		t.Errorf("no progress was reported")
	}
	cleaned, err := cleanPledges(data.Pledges)
	if err != nil || cleaned[0].Items[0].Manufacturer != "Drake Interplanetary" || cleaned[0].Items[0].Image == "" {
		t.Fatalf("pledges as read: %+v %v", cleaned[0].Items[0], err)
	}
}
