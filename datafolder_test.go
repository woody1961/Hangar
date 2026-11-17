package main

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// useTempHangar points Hangar at a fresh folder with an empty database, as a first start would.
func useTempHangar(t *testing.T) string {
	home := t.TempDir()
	t.Setenv("HANGAR_SETTINGS", "")
	t.Setenv("SHIPS_DATA", "")
	baseDir = home
	setDataDir(chooseDataDir())
	conn, err := connect()
	if err != nil {
		t.Fatal(err)
	}
	store = conn
	t.Cleanup(func() { conn.Close() })
	return home
}

// A Hangar folder copied to a share (settings.json and data/ inside) can be chosen as it is, and the next start uses it.
func TestChoosingACopiedHangarFolderSticks(t *testing.T) {
	useTempHangar(t)
	share := filepath.Join(t.TempDir(), "windows_shares", "Hangar #1") // a "#" in the name, as folder names may have
	if err := os.MkdirAll(filepath.Join(share, "data", "images"), 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(share, "settings.json"), []byte(`{"data_dir": null}`), 0o600)
	if err := copyDatabase(store, filepath.Join(share, "data", "ships.db")); err != nil {
		t.Fatal(err)
	}

	r, err := moveDataDir(share)
	if err != nil {
		t.Fatalf("the copied Hangar folder was refused: %v", err)
	}
	want := filepath.Join(share, "data")
	if r["mode"] != "switch" || r["target"] != want {
		t.Fatalf("move result %v", r)
	}
	// the next start reads the setting and opens the share's database
	if got := chooseDataDir(); got != want {
		t.Fatalf("next start uses %q, want %q", got, want)
	}
	setDataDir(chooseDataDir())
	conn, err := connect()
	if err != nil {
		t.Fatalf("the share's database didn't open: %v", err)
	}
	conn.Close()
}

// A folder with something else in it is refused with a reason that says what to choose.
func TestAFolderWithOtherThingsIsRefusedClearly(t *testing.T) {
	useTempHangar(t)
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, "notes.txt"), []byte("x"), 0o600)
	if _, err := moveDataDir(other); err == nil || !strings.Contains(err.Error(), "no Hangar data") {
		t.Fatalf("got %v", err)
	}
}

// The names SQLite is given: a plain path on Windows (so \\server\share works), an escaped address elsewhere.
func TestDatabaseNames(t *testing.T) {
	q := func() url.Values { v := url.Values{}; v.Add("_pragma", "foreign_keys(1)"); return v }
	if got := sqliteName("windows", `\\freenas-1\windows_shares\Hangar\data\ships.db`, false, q()); !strings.HasPrefix(got, `\\freenas-1\windows_shares\Hangar\data\ships.db?`) {
		t.Errorf("windows network path: %s", got)
	}
	if got := sqliteName("windows", `Z:\Hangar\data\ships.db`, true, q()); !strings.Contains(got, "_query_only=1") || strings.HasPrefix(got, "file:") {
		t.Errorf("windows read-only: %s", got)
	}
	if got := sqliteName("darwin", "/Volumes/share/Hangar #1/50%?/ships.db", true, q()); got != "file:///Volumes/share/Hangar %231/50%25%3f/ships.db?_pragma=foreign_keys%281%29&mode=ro&vfs=unix-flock" {
		t.Errorf("mac: %s", got)
	}
	// and a real one with odd characters in its folder opens, read-write and read-only
	dir := filepath.Join(t.TempDir(), "odd #name ? 100%")
	os.MkdirAll(dir, 0o700)
	db, err := openDB(filepath.Join(dir, "ships.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec("CREATE TABLE packages (id INTEGER); CREATE TABLE items (id INTEGER)")
	db.Close()
	if _, _, err := countHangar(filepath.Join(dir, "ships.db")); err != nil {
		t.Fatalf("read-only open: %v", err)
	}
}

// Another computer's fresh note means the folder is open there; an old one, or our own, doesn't.
func TestOpenOnAnotherComputer(t *testing.T) {
	useTempHangar(t)
	note := func(host string, pid int, at time.Time) {
		data, _ := json.Marshal(inUseNote{Host: host, PID: pid, Time: at})
		os.WriteFile(inUsePath(), data, 0o600)
	}
	note("woodys-pc", 4242, time.Now())
	if got := readOtherUser(); got != "woodys-pc" {
		t.Fatalf("fresh note from another computer: %q", got)
	}
	note("woodys-pc", 4242, time.Now().Add(-10*time.Minute))
	if got := readOtherUser(); got != "" {
		t.Fatalf("stale note: %q", got)
	}
	note(myHost, os.Getpid(), time.Now())
	if got := readOtherUser(); got != "" {
		t.Fatalf("our own note: %q", got)
	}
	note(myHost, os.Getpid()+1, time.Now())
	if got := readOtherUser(); !strings.Contains(got, "another Hangar window") {
		t.Fatalf("second window: %q", got)
	}
}

// A database on a network share opens: HANGAR_PROBE_SHARE_DB names one (it is only read).
func TestDatabaseOnANetworkShare(t *testing.T) {
	path := os.Getenv("HANGAR_PROBE_SHARE_DB")
	if path == "" {
		t.Skip("set HANGAR_PROBE_SHARE_DB to a ships.db on a network share")
	}
	p, i, err := countHangar(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d packages, %d items", p, i)
}
