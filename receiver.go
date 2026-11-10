package main

// Work that happens away from the window: picture downloads after an import, Hangar's RSI window import, exports, and
// the small receiver on this computer that the "Import to Hangar" bookmark sends its data to. The receiver answers only
// requests addressed to localhost, and only the RSI site (or Hangar itself) may send to it.

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"modernc.org/sqlite"
)

const (
	host            = "127.0.0.1"
	maxImageBytes   = 5 * 1024 * 1024
	rsiOrigin       = "https://robertsspaceindustries.com" // the importer bookmark runs there
	importPrefix    = "/api/import/"
	importBodyLimit = 16 * 1024 * 1024
	imageWorkers    = 6
)

var httpURLPrefix = regexp.MustCompile(`^https?://`)

var imageTypes = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif"}

var exportColumns = []string{"package", "rsi_pledge_id", "pledge_date", "value", "purchase_value", "status", "status_date", "status_note",
	"gifted_to", "type", "name", "manufacturer", "purpose", "insurance", "crew", "storage_scu", "flight_ready", "notes", "image",
	"custom_name", "membership_id", "originally", "upgrade_cost", "upgrade_ids"}

var packageExport = []string{"rsi_pledge_id", "pledge_date", "value", "purchase_value", "status", "status_date", "status_note", "gifted_to"}

var (
	port           int
	allowedHosts   map[string]bool
	allowedOrigins map[string]bool
	dbMu           sync.Mutex // one shared connection; everything that touches the database holds this
	store          *sql.DB

	imageJobMu sync.Mutex
	imageJob   = map[string]int{"total": 0, "done": 0, "failed": 0}

	rsiJobMu sync.Mutex
	rsiJob   = map[string]any{"state": "idle"} // idle | reading | saving | done | error

	receiver    *http.Server
	receiverErr string // why the bookmark can't reach Hangar, when it can't
)

func setPort(p int) {
	port = p
	allowedHosts = map[string]bool{fmt.Sprintf("127.0.0.1:%d", p): true, fmt.Sprintf("localhost:%d", p): true}
	allowedOrigins = map[string]bool{fmt.Sprintf("http://127.0.0.1:%d", p): true, fmt.Sprintf("http://localhost:%d", p): true}
}

func configuredPort() int {
	if p, err := strconv.Atoi(os.Getenv("SHIPS_PORT")); err == nil && p > 0 {
		return p
	}
	return settingInt("port")
}

func setRSIJob(fields map[string]any) {
	rsiJobMu.Lock()
	defer rsiJobMu.Unlock()
	for k, v := range fields {
		rsiJob[k] = v
	}
}

// ---------- pictures ----------

// queueImages downloads the pictures of an import in the background, unless Settings says not to.
func queueImages(tasks []imageTask, summary *importSummary) {
	if len(tasks) == 0 {
		return
	}
	if settingBool("download_pictures") {
		runImageJob(tasks)
		return
	}
	n := len(tasks)
	summary.ImagesSkipped = &n
	summary.ImagesQueued = 0
}

var downloadClient = &http.Client{Timeout: 20 * time.Second}

func downloadImage(rawURL string) ([]byte, string, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Ships hangar app)")
	resp, err := downloadClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("HTTP Error %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	return data, resp.Header.Get("Content-Type"), err
}

// downloadWithRetry tries a picture again when the other site is briefly unavailable or busy (a first import asks
// for hundreds at once); a missing picture (404) isn't retried.
func downloadWithRetry(rawURL string) ([]byte, string, error) {
	var lastErr error
	for attempt, wait := 0, time.Second; attempt < 3; attempt, wait = attempt+1, wait*3 {
		data, ctype, err := downloadImage(rawURL)
		if err == nil {
			return data, ctype, nil
		}
		lastErr = err
		msg := err.Error()
		if strings.Contains(msg, "HTTP Error 4") && !strings.Contains(msg, "HTTP Error 429") {
			break
		}
		time.Sleep(wait)
	}
	return nil, "", lastErr
}

// saveItemImage stores image bytes for an item. The caller holds dbMu.
func saveItemImage(id int64, data []byte, ctype string) (row, error) {
	ext := imageTypes[strings.ToLower(strings.TrimSpace(strings.Split(ctype, ";")[0]))]
	if ext == "" {
		return nil, userErr("Unsupported image type")
	}
	if len(data) == 0 || len(data) > maxImageBytes {
		return nil, userErr("Image is empty or larger than 5 MB")
	}
	if getRow(store, "items", id) == nil {
		return nil, errNotFound
	}
	token := make([]byte, 4)
	rand.Read(token)
	filename := fmt.Sprintf("item%d-%s%s", id, hex.EncodeToString(token), ext)
	if err := os.WriteFile(filepath.Join(imageDir, filename), data, 0o644); err != nil {
		return nil, err
	}
	return setItemImage(store, id, filename)
}

// runImageJob downloads pictures for imported items in the background; each unique address is fetched once.
func runImageJob(tasks []imageTask) {
	byURL, order := map[string][]int64{}, []string{}
	for _, t := range tasks {
		if _, ok := byURL[t.url]; !ok {
			order = append(order, t.url)
		}
		byURL[t.url] = append(byURL[t.url], t.itemID)
	}
	imageJobMu.Lock()
	imageJob["total"] += len(tasks)
	imageJobMu.Unlock()
	work := make(chan string)
	go func() {
		var wg sync.WaitGroup
		for i := 0; i < imageWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for u := range work {
					data, ctype, err := downloadWithRetry(u)
					for _, id := range byURL[u] {
						ok := false
						if err == nil && len(data) > 0 {
							dbMu.Lock()
							if r := getRow(store, "items", id); r != nil && str(r["image"]) == "" {
								_, saveErr := saveItemImage(id, data, ctype)
								ok = saveErr == nil
							} else if r != nil {
								ok = true
							}
							dbMu.Unlock()
						}
						imageJobMu.Lock()
						if ok {
							imageJob["done"]++
						} else {
							imageJob["failed"]++
						}
						imageJobMu.Unlock()
					}
				}
			}()
		}
		for _, u := range order {
			work <- u
		}
		close(work)
		wg.Wait()
	}()
}

func imageStatus() map[string]any {
	imageJobMu.Lock()
	defer imageJobMu.Unlock()
	done := imageJob["done"] + imageJob["failed"]
	return map[string]any{"total": imageJob["total"], "done": imageJob["done"], "failed": imageJob["failed"], "running": done < imageJob["total"]}
}

// ---------- the RSI window import ----------

// runRSIJob reads the pledges and log from Hangar's RSI window, then imports them. Runs in the background.
func runRSIJob() {
	data, err := window.read(func(stage string, cur, total int) {
		setRSIJob(map[string]any{"stage": stage, "current": cur, "total": total})
	})
	if err == nil {
		setRSIJob(map[string]any{"state": "saving"})
		var summary *importSummary
		var tasks []imageTask
		dbMu.Lock()
		summary, tasks, err = importPledges(data.Pledges, data.Upgrades, data.Events, pyText(data.LogNote) == "")
		dbMu.Unlock()
		if err == nil {
			queueImages(tasks, summary)
			summary.LogNote = data.LogNote
			setRSIJob(map[string]any{"state": "done", "summary": summary})
			if settingBool("rsi_close_after_import") {
				window.close() // the saved sign-in survives a close
			}
			return
		}
	}
	var ns notSignedIn
	if errors.As(err, &ns) {
		setRSIJob(map[string]any{"state": "error", "error": err.Error(), "kind": "not_signed_in"})
		return
	}
	setRSIJob(map[string]any{"state": "error", "error": errorText(err), "kind": "other"})
}

// errorText is what the window shows for an error.
func errorText(err error) string {
	var ue userError
	if errors.As(err, &ue) {
		return ue.msg
	}
	if errors.Is(err, errNotFound) {
		return "That was deleted in the meantime."
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		return dbErrorText(err)
	}
	return "Error: " + err.Error()
}

func dbErrorText(err error) string {
	text := err.Error()
	if strings.Contains(text, "UNIQUE") && strings.Contains(text, "rsi_pledge_id") {
		return "Another package already has that pledge ID."
	}
	if strings.Contains(text, "FOREIGN KEY") {
		return "That package doesn't exist."
	}
	return "Could not save that: " + text
}

func portIsFree(p int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, p))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// ---------- the bookmark receiver ----------

func send(w http.ResponseWriter, r *http.Request, status int, body any, headers map[string]string) {
	data, _ := json.Marshal(body)
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	if strings.HasPrefix(r.URL.Path, importPrefix) && r.Header.Get("Origin") == rsiOrigin {
		h.Set("Access-Control-Allow-Origin", rsiOrigin)
		h.Set("Access-Control-Allow-Private-Network", "true")
		h.Set("Vary", "Origin")
	}
	for k, v := range headers {
		h.Set(k, v)
	}
	w.WriteHeader(status)
	if status != http.StatusNoContent {
		w.Write(data)
	}
}

func sendError(w http.ResponseWriter, r *http.Request, status int, message string) {
	send(w, r, status, map[string]string{"error": message}, nil)
}

// guard: only requests addressed to localhost (blocks DNS rebinding), a custom header on writes so other websites
// can't silently post here, and only the RSI site or Hangar itself as the sender.
func guard(w http.ResponseWriter, r *http.Request, mutating bool) bool {
	if !allowedHosts[r.Host] {
		sendError(w, r, 403, "Forbidden host")
		return false
	}
	if mutating && r.Header.Get("X-Ships") != "1" {
		sendError(w, r, 403, "Missing X-Ships header")
		return false
	}
	origin := r.Header.Get("Origin")
	if mutating && origin != "" && !allowedOrigins[origin] && !(origin == rsiOrigin && strings.HasPrefix(r.URL.Path, importPrefix)) {
		sendError(w, r, 403, "Forbidden origin")
		return false
	}
	return true
}

func readBody(r *http.Request, limit int64) ([]byte, error) {
	if r.ContentLength > limit {
		return nil, userErr("Request too large")
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, userErr("Bad request body")
	}
	if int64(len(data)) > limit {
		return nil, userErr("Request too large")
	}
	return data, nil
}

// receiverHandler answers the bookmark: a preflight, the import itself, the picture progress, and "is Hangar here?".
func receiverHandler(onImport func(*importSummary)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "OPTIONS":
			if !guard(w, r, false) {
				return
			}
			if strings.HasPrefix(r.URL.Path, importPrefix) && r.Header.Get("Origin") == rsiOrigin {
				send(w, r, 204, nil, map[string]string{"Access-Control-Allow-Methods": "GET, POST",
					"Access-Control-Allow-Headers": "Content-Type, X-Ships", "Access-Control-Max-Age": "600"})
				return
			}
			sendError(w, r, 403, "Forbidden")
		case r.Method == "GET" && r.URL.Path == "/api/ping":
			if guard(w, r, false) {
				send(w, r, 200, map[string]string{"app": "hangar", "version": version}, nil)
			}
		case r.Method == "GET" && r.URL.Path == importPrefix+"status":
			if guard(w, r, false) {
				send(w, r, 200, imageStatus(), nil)
			}
		case r.Method == "POST" && r.URL.Path == importPrefix+"rsi":
			if !guard(w, r, true) {
				return
			}
			data, err := readBody(r, importBodyLimit)
			var body map[string]any
			if err == nil {
				dec := json.NewDecoder(bytes.NewReader(data))
				dec.UseNumber()
				if dec.Decode(&body) != nil {
					err = userErr("That request isn't valid JSON")
				}
			}
			if err != nil {
				sendError(w, r, 400, errorText(err))
				return
			}
			dbMu.Lock()
			summary, tasks, err := importPledges(body["pledges"], body["upgrades"], body["events"], body["complete"] == true)
			dbMu.Unlock()
			if err != nil {
				var ue userError
				if errors.As(err, &ue) {
					sendError(w, r, 400, ue.msg)
				} else {
					log.Printf("bookmark import: %v", err)
					sendError(w, r, 500, errorText(err))
				}
				return
			}
			queueImages(tasks, summary)
			onImport(summary)
			send(w, r, 200, summary, nil)
		default:
			sendError(w, r, 404, "Not found")
		}
	})
}

// startReceiver listens for the bookmark. If the port is taken, Hangar still works; only the bookmark can't reach it.
func startReceiver(a *App) {
	l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		receiverErr = fmt.Sprintf("port %d is used by another program; change the port in Settings", port)
		return
	}
	receiver = &http.Server{ReadHeaderTimeout: 30 * time.Second, Handler: receiverHandler(func(s *importSummary) {
		a.post(func() {
			a.reload()
			a.toast(fmt.Sprintf("Imported %d pledges from the bookmark", s.Pledges))
			if a.rsiDlg != nil {
				a.rsiDlg.showResult(a, s)
			}
		})
	})}
	go receiver.Serve(l)
}

// ---------- exports ----------

func exportJSON() ([]byte, error) {
	dbMu.Lock()
	data, err := allData(store)
	dbMu.Unlock()
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(data, "", "  ")
}

// writeCSV writes one row per item, with its package's details, for spreadsheet apps.
func writeCSV(out io.Writer) error {
	dbMu.Lock()
	data, err := allData(store)
	dbMu.Unlock()
	if err != nil {
		return err
	}
	packages := map[int64]row{}
	for _, p := range data["packages"].([]row) {
		packages[p["id"].(int64)] = p
	}
	out.Write([]byte("\xef\xbb\xbf")) // so spreadsheet apps open the UTF-8 file correctly
	cw := csv.NewWriter(out)
	cw.UseCRLF = true
	cw.Write(exportColumns)
	middle := exportColumns[len(packageExport)+1 : len(exportColumns)-3]
	for _, item := range data["items"].([]row) {
		pid, _ := item["package_id"].(int64)
		p := packages[pid]
		var history []map[string]any
		if s := str(item["upgrade_history"]); s != "" {
			json.Unmarshal([]byte(s), &history)
		}
		rec := []string{csvValue(p["name"])}
		for _, k := range packageExport {
			rec = append(rec, csvValue(p[k]))
		}
		for _, k := range middle {
			rec = append(rec, csvValue(item[k]))
		}
		originally, cost, ids := "", 0.0, []string{}
		if len(history) > 0 {
			originally = pyText(history[0]["from"])
		}
		for _, h := range history {
			if m, ok := money(pyText(h["price"])); ok {
				cost += m
			}
			if id := pyText(h["upgrade_id"]); id != "" {
				ids = append(ids, id)
			}
		}
		costText := ""
		if cost != 0 {
			costText = pyFloat(cost)
		}
		rec = append(rec, originally, costText, strings.Join(ids, " "))
		cw.Write(rec)
	}
	cw.Flush()
	return cw.Error()
}

func csvValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return pyFloat(x)
	case string:
		return x
	}
	return fmt.Sprint(v)
}
