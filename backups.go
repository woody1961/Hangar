package main

// The data folder around the database: backups (daily, before an import, before a restore, by hand), restore,
// "Start fresh" archives and moving the folder elsewhere.

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	dailyRe    = regexp.MustCompile(`^ships-\d{4}-\d{2}-\d{2}\.db$`)
	snapRe     = regexp.MustCompile(`^(.+)-(\d{8}-\d{6})(?:-\d+)?\.db$`)
	snapOrdRe  = regexp.MustCompile(`(\d{8}-\d{6})(?:-(\d+))?\.db$`)
	backupKind = map[string]string{"daily": "Daily", "before-rsi-import": "Before an RSI import",
		"before-restore": "Before a restore", "manual": "Backed up by you"}
)

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// copyDatabase writes a consistent copy of a database to target (which must not exist yet).
func copyDatabase(src *sql.DB, target string) error {
	_, err := src.Exec("VACUUM INTO ?", target)
	return err
}

// copyDatabaseFile copies the database file at path without keeping it open.
func copyDatabaseFile(path, target string) error {
	src, err := openDB(path, true)
	if err != nil {
		return err
	}
	defer src.Close()
	return copyDatabase(src, target)
}

func snapshotOrder(name string) (string, int) {
	m := snapOrdRe.FindStringSubmatch(name)
	if m == nil {
		return "", 0
	}
	n := 1
	if m[2] != "" {
		n, _ = strconv.Atoi(m[2])
	}
	return m[1], n
}

// snapshot copies the database to backups/<label>-<timestamp>.db before a bulk change. With keep < 0 the Settings
// value decides how many copies with the same label stay; keep == 0 keeps them all.
func snapshot(label string, keep int) (string, error) {
	if !fileExists(dbPath) {
		return "", nil
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", err
	}
	stamp := time.Now().Format("20060102-150405")
	target, n := filepath.Join(backupDir, fmt.Sprintf("%s-%s.db", label, stamp)), 1
	for fileExists(target) { // two in the same second must not overwrite each other
		n++
		target = filepath.Join(backupDir, fmt.Sprintf("%s-%s-%d.db", label, stamp, n))
	}
	var err error
	if store != nil {
		err = copyDatabase(store, target)
	} else {
		err = copyDatabaseFile(dbPath, target)
	}
	if err != nil {
		return "", err
	}
	if keep < 0 {
		keep = settingInt("backups_snapshots")
	}
	if keep > 0 {
		entries, _ := os.ReadDir(backupDir)
		mine := []string{}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), label+"-") && strings.HasSuffix(e.Name(), ".db") {
				mine = append(mine, e.Name())
			}
		}
		sort.SliceStable(mine, func(i, j int) bool {
			a1, a2 := snapshotOrder(mine[i])
			b1, b2 := snapshotOrder(mine[j])
			if a1 != b1 {
				return a1 < b1
			}
			return a2 < b2
		})
		for i := 0; i < len(mine)-keep; i++ {
			os.Remove(filepath.Join(backupDir, mine[i]))
		}
	}
	return target, nil
}

// dailyBackup copies the database to backups/ once per day, keeping the newest few (a Settings value).
func dailyBackup() {
	if !fileExists(dbPath) {
		return
	}
	os.MkdirAll(backupDir, 0o755)
	target := filepath.Join(backupDir, "ships-"+time.Now().Format("2006-01-02")+".db")
	if !fileExists(target) {
		if err := copyDatabaseFile(dbPath, target); err != nil {
			fmt.Println("Couldn't make today's backup:", err)
		}
	}
	entries, _ := os.ReadDir(backupDir)
	daily := []string{}
	for _, e := range entries {
		if dailyRe.MatchString(e.Name()) {
			daily = append(daily, e.Name())
		}
	}
	sort.Strings(daily)
	for i := 0; i < len(daily)-settingInt("backups_daily"); i++ {
		os.Remove(filepath.Join(backupDir, daily[i]))
	}
}

func isoMinutes(t time.Time) string { return t.Format("2006-01-02T15:04") }

// countRows reads how many packages and items a database file holds (nil, nil when it can't be read).
func countRows(path string) (any, any) {
	c, err := openDB(path, true)
	if err != nil {
		return nil, nil
	}
	defer c.Close()
	var p, i int64
	if c.QueryRow("SELECT COUNT(*) FROM packages").Scan(&p) != nil || c.QueryRow("SELECT COUNT(*) FROM items").Scan(&i) != nil {
		return nil, nil
	}
	return p, i
}

// listBackups returns every backup file, newest first.
func listBackups() []map[string]any {
	out := []map[string]any{}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".db") || e.IsDir() {
			continue
		}
		path := filepath.Join(backupDir, name)
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		kind, created := "other", isoMinutes(st.ModTime())
		if dailyRe.MatchString(name) {
			kind = "daily"
		} else if m := snapRe.FindStringSubmatch(name); m != nil {
			kind = m[1]
			if t, err := time.ParseInLocation("20060102-150405", m[2], time.Local); err == nil {
				created = isoMinutes(t)
			}
		}
		label := backupKind[kind]
		if label == "" {
			label = kind
		}
		packages, items := countRows(path)
		out = append(out, map[string]any{"name": name, "kind": kind, "label": label, "created": created,
			"size": st.Size(), "packages": packages, "items": items})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a["created"] != b["created"] {
			return a["created"].(string) > b["created"].(string)
		}
		a1, a2 := snapshotOrder(a["name"].(string))
		b1, b2 := snapshotOrder(b["name"].(string))
		if a1 != b1 {
			return a1 > b1
		}
		if a2 != b2 {
			return a2 > b2
		}
		return a["name"].(string) > b["name"].(string)
	})
	return out
}

func backupPath(name any) (string, error) {
	s, ok := name.(string)
	if !ok || filepath.Base(s) != s || !strings.HasSuffix(s, ".db") || strings.ContainsAny(s, `/\`) {
		return "", userErr("That is not a backup file")
	}
	p := filepath.Join(backupDir, s)
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return "", errNotFound
	}
	return p, nil
}

// restoreBackup replaces the current database with a backup. The current one is saved first (before-restore), so a
// restore can itself be undone. Pictures are not part of a backup: they stay as they are.
func restoreBackup(name any) (map[string]any, error) {
	path, err := backupPath(name)
	if err != nil {
		return nil, err
	}
	src, err := openDB(path, true)
	if err != nil {
		return nil, userErr("That file is not a Hangar database")
	}
	var check string
	if err := src.QueryRow("PRAGMA quick_check").Scan(&check); err != nil {
		src.Close()
		return nil, userErr("That file is not a Hangar database")
	}
	if check != "ok" {
		src.Close()
		return nil, userErr("That backup file is damaged")
	}
	if _, err := src.Exec("SELECT COUNT(*) FROM packages"); err != nil {
		src.Close()
		return nil, userErr("That file is not a Hangar database")
	}
	// A fresh copy of the backup goes in place of the database, so the backup file itself is never touched.
	tmp := dbPath + ".restoring"
	os.Remove(tmp)
	err = copyDatabase(src, tmp)
	src.Close()
	if err != nil {
		return nil, err
	}
	saved, err := snapshot("before-restore", -1)
	if err != nil {
		os.Remove(tmp)
		return nil, err
	}
	if err := replaceDatabase(tmp); err != nil {
		return nil, err
	}
	safety := any(nil)
	if saved != "" {
		safety = filepath.Base(saved)
	}
	return map[string]any{"restored": name, "safety_copy": safety,
		"packages": queryInt(store, "SELECT COUNT(*) FROM packages"), "items": queryInt(store, "SELECT COUNT(*) FROM items")}, nil
}

// replaceDatabase closes the database, puts the file at newFile in its place and opens it again (bringing an older
// backup up to date with the columns Hangar has since gained). The caller holds dbMu.
func replaceDatabase(newFile string) error {
	store.Close()
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		os.Remove(dbPath + suffix)
	}
	renameErr := os.Rename(newFile, dbPath)
	c, err := connect()
	if err != nil {
		return err
	}
	store = c
	return renameErr
}

// freshStart archives the current database and pictures under data/archive/<timestamp>/ and begins with an empty
// database. Nothing is destroyed: copy the files back to return to the old data.
func freshStart() (string, error) {
	folder := filepath.Join(dataDir, "archive", time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", err
	}
	if err := copyDatabase(store, filepath.Join(folder, "ships.db")); err != nil {
		return "", err
	}
	if isDir(imageDir) {
		if err := os.Rename(imageDir, filepath.Join(folder, "images")); err != nil {
			return "", err
		}
	}
	os.MkdirAll(imageDir, 0o755)
	tx, err := store.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	for _, q := range []string{"DELETE FROM items", "DELETE FROM packages", "DELETE FROM meta"} {
		if _, err := tx.Exec(q); err != nil {
			return "", err
		}
	}
	// an RSI-first database also remembers pledges that have left the hangar
	if err := setMeta(tx, "full_history", "1"); err != nil {
		return "", err
	}
	return folder, tx.Commit()
}

func realPath(p string) string {
	a := absPath(p)
	if r, err := filepath.EvalSymlinks(a); err == nil {
		return r
	}
	return a
}

func inside(child, parent string) bool {
	c, p := realPath(child), realPath(parent)
	return c == p || strings.HasPrefix(c, p+string(os.PathSeparator))
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil // links and sockets (a browser profile has some) are not data
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func canWrite(folder string) bool {
	f, err := os.CreateTemp(folder, ".hangar-write-test-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// moveDataDir copies the data (database, pictures, backups, archives, saved RSI sign-in) to another folder and makes
// it the data folder from the next start. The old folder is left alone until you delete it. If the target already
// holds a Hangar database, Hangar simply switches to it and copies nothing.
func moveDataDir(targetArg any) (map[string]any, error) {
	s, ok := targetArg.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil, userErr("Enter a folder")
	}
	target := absPath(expandUser(strings.TrimSpace(s)))
	if inside(target, dataDir) || inside(dataDir, target) {
		return nil, userErr("Choose a folder that is not inside the current data folder, and does not contain it")
	}
	if realPath(target) == realPath(baseDir) {
		return nil, userErr("Choose a folder outside the Hangar program folder")
	}
	if fileExists(target) && !isDir(target) {
		return nil, userErr("That path is a file, not a folder")
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return nil, userErr("Hangar can't write to that folder")
	}
	if !canWrite(target) {
		return nil, userErr("Hangar can't write to that folder")
	}
	existing := filepath.Join(target, "ships.db")
	if fileExists(existing) {
		if p, _ := countRows(existing); p == nil {
			return nil, userErr("The database in that folder isn't a Hangar database")
		}
		if err := setInternalSettings(map[string]any{"data_dir": target, "previous_data_dir": nil}); err != nil {
			return nil, err
		}
		return map[string]any{"mode": "switch", "target": target, "previous": nil}, nil
	}
	if entries, _ := os.ReadDir(target); len(entries) > 0 {
		return nil, userErr("That folder isn't empty. Choose an empty folder, or one that already holds a Hangar database.")
	}
	if err := copyDatabase(store, existing); err != nil {
		return nil, err
	}
	p, i := countRows(existing)
	if p != queryInt(store, "SELECT COUNT(*) FROM packages") || i != queryInt(store, "SELECT COUNT(*) FROM items") {
		os.Remove(existing)
		return nil, userErr("The copy did not match the original, so nothing was changed")
	}
	for _, sub := range []string{"images", "backups", "archive", "rsi-browser-profile"} {
		if isDir(filepath.Join(dataDir, sub)) {
			if err := copyTree(filepath.Join(dataDir, sub), filepath.Join(target, sub)); err != nil {
				return nil, err
			}
		}
	}
	protect(target)
	if err := setInternalSettings(map[string]any{"data_dir": target, "previous_data_dir": dataDir}); err != nil {
		return nil, err
	}
	return map[string]any{"mode": "copy", "target": target, "previous": dataDir}, nil
}

// deletePreviousDataDir deletes the folder a move left behind, only if it is still recognisably a Hangar data folder
// and not the one in use.
func deletePreviousDataDir() (string, error) {
	prev := settingString("previous_data_dir")
	if prev == "" || !isDir(prev) {
		return "", userErr("There is no old data folder to delete")
	}
	if realPath(prev) == realPath(dataDir) || inside(dataDir, prev) {
		return "", userErr("That folder is the one Hangar is using")
	}
	if !fileExists(filepath.Join(prev, "ships.db")) {
		return "", userErr("That folder doesn't look like a Hangar data folder, so it was not deleted")
	}
	if err := os.RemoveAll(prev); err != nil {
		return "", err
	}
	return prev, setInternalSettings(map[string]any{"previous_data_dir": nil})
}
