package main

// SQLite storage for packages (pledges) and the items inside them. The tables are the same as the Python Hangar's,
// so a data folder made by either one opens in the other.

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	_ "modernc.org/sqlite"
)

var packageFields = []string{"name", "rsi_pledge_id", "pledge_date", "value", "purchase_value", "notes", "status",
	"status_date", "status_note", "gifted_to"}

// owned = in the hangar now; the rest are "former": melted (reclaimed for store credit), gifted (given to someone
// else), missing (gone from the RSI hangar and the log doesn't say why)
var statuses = []string{"owned", "melted", "gifted", "missing"}

var itemFields = []string{"package_id", "type", "name", "manufacturer", "purpose", "insurance", "crew", "storage_scu",
	"flight_ready", "notes"}

const schema = `
CREATE TABLE IF NOT EXISTS packages (
    id            INTEGER PRIMARY KEY,
    name          TEXT NOT NULL,
    rsi_pledge_id TEXT UNIQUE,
    pledge_date   TEXT,
    value         TEXT,
    notes         TEXT,
    sort_order    INTEGER,
    status        TEXT NOT NULL DEFAULT 'owned',
    status_date   TEXT,
    status_note   TEXT,
    gifted_to     TEXT,
    purchase_value TEXT,
    gift_pending  INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT
);
CREATE TABLE IF NOT EXISTS items (
    id            INTEGER PRIMARY KEY,
    package_id    INTEGER REFERENCES packages(id) ON DELETE CASCADE,
    type          TEXT,
    name          TEXT NOT NULL,
    manufacturer  TEXT,
    purpose       TEXT,
    insurance     TEXT,
    crew          TEXT,
    storage_scu   TEXT,
    flight_ready  TEXT,
    notes         TEXT,
    image         TEXT,
    sort_order    INTEGER,
    upgrade_history TEXT,
    custom_name   TEXT,
    membership_id TEXT,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS items_package ON items(package_id);
`

// Paths of the data folder, decided once at start: SHIPS_DATA wins, then the folder chosen in Settings, then data/
// next to the program. Changing it takes a restart.
var (
	dataDir   string
	dbPath    string
	imageDir  string
	backupDir string
)

func setDataDir(dir string) {
	dataDir = dir
	dbPath = filepath.Join(dir, "ships.db")
	imageDir = filepath.Join(dir, "images")
	backupDir = filepath.Join(dir, "backups")
}

func chooseDataDir() string {
	if d := os.Getenv("SHIPS_DATA"); d != "" {
		return absPath(d)
	}
	if d := settingString("data_dir"); d != "" {
		return absPath(expandUser(d))
	}
	return filepath.Join(baseDir, "data")
}

func expandUser(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

func absPath(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// protect keeps other people on this computer out of the data folder: it holds the database (with gift recipients'
// addresses) and Hangar's saved RSI sign-in. Only a folder that is yours is changed; failing is not an error.
func protect(folder string) {
	if runtime.GOOS == "windows" {
		return
	}
	if ownedByMe(folder) {
		os.Chmod(folder, 0o700)
	}
}

// openDB opens a SQLite file the way Hangar uses it: foreign keys on, and a wait instead of an error when busy.
func openDB(path string, readOnly bool) (*sql.DB, error) {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	if readOnly {
		q.Add("mode", "ro")
	}
	conn, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1) // one shared connection guarded by dbMu, as requests are tiny
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func connect() (*sql.DB, error) {
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		return nil, err
	}
	protect(dataDir)
	conn, err := openDB(dbPath, false)
	if err != nil {
		return nil, err
	}
	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func columnSet(q querier, table string) map[string]bool {
	out := map[string]bool{}
	rows, err := q.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		rows.Scan(&n)
		out[n] = true
	}
	return out
}

// migrate creates what is missing and brings an older database up to date.
func migrate(conn *sql.DB) error {
	if _, err := conn.Exec(schema); err != nil {
		return err
	}
	itemCols := columnSet(conn, "items")
	for _, col := range []string{"upgrade_history", "custom_name", "membership_id", "storage_scu"} {
		if !itemCols[col] {
			if _, err := conn.Exec("ALTER TABLE items ADD COLUMN " + col + " TEXT"); err != nil {
				return err
			}
		}
	}
	pkgCols := columnSet(conn, "packages")
	for _, c := range [][2]string{{"status", "TEXT NOT NULL DEFAULT 'owned'"}, {"status_date", "TEXT"}, {"status_note", "TEXT"},
		{"gifted_to", "TEXT"}, {"purchase_value", "TEXT"}, {"gift_pending", "INTEGER NOT NULL DEFAULT 0"}} {
		if !pkgCols[c[0]] {
			if _, err := conn.Exec("ALTER TABLE packages ADD COLUMN " + c[0] + " " + c[1]); err != nil {
				return err
			}
		}
	}
	// Insurance used to be stored as "L" (and bare month counts); it now reads as the words
	if _, err := conn.Exec("UPDATE items SET insurance = 'Lifetime' WHERE insurance = 'L'"); err != nil {
		return err
	}
	_, err := conn.Exec("UPDATE items SET insurance = insurance || ' months' WHERE insurance != '' AND insurance NOT GLOB '*[^0-9]*'")
	return err
}

// querier is what both a connection and a transaction can do.
type querier interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

type row = map[string]any

// queryRows reads every row as column -> value (int64, float64, string or nil), like the Python version's dicts.
func queryRows(q querier, query string, args ...any) ([]row, error) {
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []row{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		r := row{}
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				vals[i] = string(b)
			}
			r[c] = vals[i]
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func mustRows(q querier, query string, args ...any) []row {
	r, err := queryRows(q, query, args...)
	if err != nil {
		panic(err)
	}
	return r
}

func queryInt(q querier, query string, args ...any) int64 {
	var n sql.NullInt64
	if err := q.QueryRow(query, args...).Scan(&n); err != nil {
		panic(err)
	}
	return n.Int64
}

func mustExec(q querier, query string, args ...any) sql.Result {
	r, err := q.Exec(query, args...)
	if err != nil {
		panic(err)
	}
	return r
}

func getMeta(q querier, key string) (string, bool) {
	var v sql.NullString
	err := q.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&v)
	if err != nil {
		return "", false
	}
	return v.String, true
}

func setMeta(q querier, key, value string) error {
	_, err := q.Exec("INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value)
	return err
}

func allData(q querier) (map[string]any, error) {
	packages, err := queryRows(q, "SELECT * FROM packages ORDER BY sort_order, id")
	if err != nil {
		return nil, err
	}
	items, err := queryRows(q, "SELECT * FROM items ORDER BY package_id, sort_order, id")
	if err != nil {
		return nil, err
	}
	return map[string]any{"packages": packages, "items": items}, nil
}

func getRow(q querier, table string, id int64) row {
	rows, err := queryRows(q, "SELECT * FROM "+table+" WHERE id = ?", id)
	if err != nil || len(rows) == 0 {
		return nil
	}
	return rows[0]
}

// cleanFields keeps the known fields of a request: text is trimmed and blank text becomes null.
func cleanFields(data map[string]any, fields []string) ([]string, map[string]any) {
	keys := []string{}
	out := map[string]any{}
	for _, f := range fields {
		v, ok := data[f]
		if !ok {
			continue
		}
		if s, isStr := v.(string); isStr {
			s = strings.TrimSpace(s)
			if s == "" {
				v = nil
			} else {
				v = s
			}
		}
		keys = append(keys, f)
		out[f] = v
	}
	return keys, out
}

func checkStatus(keys []string, values map[string]any) ([]string, error) {
	v, ok := values["status"]
	if !ok {
		return keys, nil
	}
	if v == nil { // a blank means "leave as is" (new packages default to owned)
		delete(values, "status")
		return without(keys, "status"), nil
	}
	s, _ := v.(string)
	if !contains(statuses, s) {
		return nil, userErr("Status must be one of: %s", strings.Join(statuses, ", "))
	}
	return keys, nil
}

func without(keys []string, k string) []string {
	out := []string{}
	for _, x := range keys {
		if x != k {
			out = append(out, x)
		}
	}
	return out
}

func insertRow(q querier, table string, keys []string, values map[string]any) (int64, error) {
	args := make([]any, len(keys))
	marks := make([]string, len(keys))
	for i, k := range keys {
		args[i] = sqlValue(values[k])
		marks[i] = "?"
	}
	res, err := q.Exec(fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(keys, ", "), strings.Join(marks, ", ")), args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// sqlValue turns a JSON value into something SQLite stores the way Python's sqlite3 would.
func sqlValue(v any) any {
	switch x := v.(type) {
	case bool:
		if x {
			return 1
		}
		return 0
	case float64:
		if x == float64(int64(x)) {
			return int64(x)
		}
		return x
	case map[string]any, []any:
		return fmt.Sprint(x)
	}
	return v
}

func updateRow(q querier, table string, id int64, keys []string, values map[string]any) error {
	if getRow(q, table, id) == nil {
		return errNotFound
	}
	if len(keys) == 0 {
		return nil
	}
	sets := make([]string, len(keys))
	args := make([]any, 0, len(keys)+1)
	for i, k := range keys {
		sets[i] = k + " = ?"
		args = append(args, sqlValue(values[k]))
	}
	args = append(args, id)
	_, err := q.Exec(fmt.Sprintf("UPDATE %s SET %s, updated_at = datetime('now') WHERE id = ?", table, strings.Join(sets, ", ")), args...)
	return err
}

func createPackage(q querier, data map[string]any) (row, error) {
	keys, values := cleanFields(data, packageFields)
	keys, err := checkStatus(keys, values)
	if err != nil {
		return nil, err
	}
	if s, _ := values["name"].(string); s == "" {
		return nil, userErr("Package name is required")
	}
	// New packages go to the top of the list
	values["sort_order"] = queryInt(q, "SELECT COALESCE(MIN(sort_order), 1) - 1 FROM packages")
	keys = append(keys, "sort_order")
	id, err := insertRow(q, "packages", keys, values)
	if err != nil {
		return nil, err
	}
	return getRow(q, "packages", id), nil
}

func updatePackage(q querier, id int64, data map[string]any) (row, error) {
	keys, values := cleanFields(data, packageFields)
	keys, err := checkStatus(keys, values)
	if err != nil {
		return nil, err
	}
	if v, ok := values["name"]; ok && v == nil {
		return nil, userErr("Package name is required")
	}
	if err := updateRow(q, "packages", id, keys, values); err != nil {
		return nil, err
	}
	return getRow(q, "packages", id), nil
}

func deletePackage(q querier, id int64) error {
	rows, err := queryRows(q, "SELECT image FROM items WHERE package_id = ? AND image IS NOT NULL", id)
	if err != nil {
		return err
	}
	if _, err := q.Exec("DELETE FROM packages WHERE id = ?", id); err != nil {
		return err
	}
	for _, r := range rows {
		removeImageFile(str(r["image"]))
	}
	return nil
}

var scuRe = regexp.MustCompile(`^\d{1,6}(?:\.\d{1,3})?$`)

// checkItem: storage is a plain number of SCU (96, 0.5), kept as typed so it reads back the way you wrote it.
func checkItem(values map[string]any) error {
	v, ok := values["storage_scu"]
	if !ok || v == nil {
		return nil
	}
	text := strings.TrimSpace(pyStr(v))
	if !scuRe.MatchString(text) {
		return userErr("Storage must be a number of SCU, for example 96 or 0.5")
	}
	values["storage_scu"] = text
	return nil
}

func createItem(q querier, data map[string]any) (row, error) {
	keys, values := cleanFields(data, itemFields)
	if err := checkItem(values); err != nil {
		return nil, err
	}
	if s, _ := values["name"].(string); s == "" {
		return nil, userErr("Item name is required")
	}
	values["sort_order"] = queryInt(q, "SELECT COALESCE(MAX(sort_order), 0) + 1 FROM items WHERE package_id IS ?", sqlValue(values["package_id"]))
	keys = append(keys, "sort_order")
	id, err := insertRow(q, "items", keys, values)
	if err != nil {
		return nil, err
	}
	return getRow(q, "items", id), nil
}

func updateItem(q querier, id int64, data map[string]any) (row, error) {
	keys, values := cleanFields(data, itemFields)
	if err := checkItem(values); err != nil {
		return nil, err
	}
	if v, ok := values["name"]; ok && v == nil {
		return nil, userErr("Item name is required")
	}
	if err := updateRow(q, "items", id, keys, values); err != nil {
		return nil, err
	}
	return getRow(q, "items", id), nil
}

func deleteItem(q querier, id int64) error {
	r := getRow(q, "items", id)
	if _, err := q.Exec("DELETE FROM items WHERE id = ?", id); err != nil {
		return err
	}
	if r != nil && r["image"] != nil {
		removeImageFile(str(r["image"]))
	}
	return nil
}

func setItemImage(q querier, id int64, filename any) (row, error) {
	r := getRow(q, "items", id)
	if r == nil {
		return nil, errNotFound
	}
	if _, err := q.Exec("UPDATE items SET image = ?, updated_at = datetime('now') WHERE id = ?", filename, id); err != nil {
		return nil, err
	}
	if old := str(r["image"]); old != "" && old != str(filename) {
		removeImageFile(old)
	}
	return getRow(q, "items", id), nil
}

func removeImageFile(name string) {
	if name == "" {
		return
	}
	os.Remove(filepath.Join(imageDir, filepath.Base(name)))
}

// str reads a text value from a row (nil and non-text become "").
func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// pyStr formats a value the way Python's str() would for the values that reach the API.
func pyStr(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return pyFloat(x)
	case bool:
		if x {
			return "True"
		}
		return "False"
	}
	return fmt.Sprint(v)
}

// Errors the API turns into answers: a userError is a 400 with its text, errNotFound a 404.
type userError struct{ msg string }

func (e userError) Error() string { return e.msg }

func userErr(format string, args ...any) error { return userError{fmt.Sprintf(format, args...)} }

var errNotFound = errors.New("not found")
