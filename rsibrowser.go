package main

// Hangar's own RSI window.
//
// A web page on localhost is not allowed to read your RSI account (browsers forbid it), so Hangar opens a separate
// Chrome/Edge/Brave window with a profile of its own, shows RSI's pledges page there, and, once you have signed in,
// reads it through the browser's remote-debugging port (which listens on this computer only). The password is typed
// into RSI's own page and never passes through Hangar. The saved sign-in lives in <data>/rsi-browser-profile/
// ("Forget my RSI sign-in" deletes it).

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

func rsiURL() string {
	if u := os.Getenv("HANGAR_RSI_URL"); u != "" {
		return u
	}
	return "https://robertsspaceindustries.com/en/account/pledges"
}

// "Is this the pledges page of a signed-in account?" The Hangar log button only exists when signed in.
const checkJS = "JSON.stringify({host: location.hostname, path: location.pathname, " +
	"signed: !!(document.querySelector('ul.list-items') || document.querySelector('a.js-pledge-log'))})"

func profileDir() string { return filepath.Join(dataDir, "rsi-browser-profile") }

type browserInfo struct{ Name, Path string }

// installedBrowsers lists the Chromium-based browsers installed on this computer.
func installedBrowsers() []browserInfo {
	var candidates []browserInfo
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		for _, app := range []string{"Google Chrome", "Microsoft Edge", "Brave Browser", "Chromium"} {
			for _, base := range []string{"/Applications", filepath.Join(home, "Applications")} {
				candidates = append(candidates, browserInfo{app, fmt.Sprintf("%s/%s.app/Contents/MacOS/%s", base, app, app)})
			}
		}
	case "windows":
		for _, v := range []string{"PROGRAMFILES", "PROGRAMFILES(X86)", "LOCALAPPDATA"} {
			if root := os.Getenv(v); root != "" {
				candidates = append(candidates,
					browserInfo{"Google Chrome", filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe")},
					browserInfo{"Microsoft Edge", filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe")},
					browserInfo{"Brave", filepath.Join(root, "BraveSoftware", "Brave-Browser", "Application", "brave.exe")})
			}
		}
	default:
		for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"} {
			if p, err := exec.LookPath(n); err == nil {
				candidates = append(candidates, browserInfo{n, p})
			}
		}
	}
	seen := map[string]bool{}
	out := []browserInfo{}
	for _, c := range candidates {
		if !seen[c.Path] && isExecutable(c.Path) {
			seen[c.Path] = true
			out = append(out, c)
		}
	}
	return out
}

// findBrowser picks the browser for Hangar's RSI window: HANGAR_BROWSER, else the one chosen in Settings, else the
// first installed Chromium-based browser.
func findBrowser() *browserInfo {
	chosen := os.Getenv("HANGAR_BROWSER")
	if chosen == "" {
		if s := settingString("rsi_browser"); s != "auto" {
			chosen = s
		}
	}
	found := installedBrowsers()
	if chosen != "" && isExecutable(chosen) {
		for _, b := range found {
			if b.Path == chosen {
				return &b
			}
		}
		return &browserInfo{"your browser", chosen}
	}
	if len(found) > 0 {
		return &found[0]
	}
	return nil
}

// maxBrowserMessage caps one message from the browser (a whole hangar read is a few megabytes).
const maxBrowserMessage = 256 << 20

// ---------- the smallest WebSocket client that can talk to the browser's debugging port ----------

type wsConn struct {
	c net.Conn
	r *bufio.Reader
}

func wsDial(rawURL string, timeout time.Duration) (*wsConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	c, err := net.DialTimeout("tcp", u.Host, timeout)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(time.Now().Add(timeout))
	keyBytes := make([]byte, 16)
	rand.Read(keyBytes)
	key := base64.StdEncoding.EncodeToString(keyBytes)
	fmt.Fprintf(c, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		u.RequestURI(), u.Host, key)
	r := bufio.NewReader(c)
	status, err := r.ReadString('\n')
	if err != nil {
		c.Close()
		return nil, errors.New("the browser closed the connection")
	}
	if !strings.Contains(status, " 101 ") {
		c.Close()
		return nil, errors.New("the browser refused the debugging connection")
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			c.Close()
			return nil, errors.New("the browser closed the connection")
		}
		if line == "\r\n" {
			break
		}
	}
	return &wsConn{c, r}, nil
}

func (w *wsConn) sendFrame(opcode byte, data []byte) error {
	header := []byte{0x80 | opcode}
	n := len(data)
	switch {
	case n < 126:
		header = append(header, 0x80|byte(n))
	case n < 65536:
		header = append(header, 0x80|126, byte(n>>8), byte(n))
	default:
		header = append(header, 0x80|127)
		header = binary.BigEndian.AppendUint64(header, uint64(n))
	}
	mask := make([]byte, 4)
	rand.Read(mask)
	masked := make([]byte, n)
	for i, b := range data {
		masked[i] = b ^ mask[i%4]
	}
	_, err := w.c.Write(append(append(header, mask...), masked...))
	return err
}

func (w *wsConn) send(text string) error { return w.sendFrame(0x1, []byte(text)) }

func (w *wsConn) recv() (string, error) {
	var message []byte
	for {
		head := make([]byte, 2)
		if _, err := io.ReadFull(w.r, head); err != nil {
			return "", errors.New("the browser closed the connection")
		}
		fin, opcode, n := head[0]&0x80 != 0, head[0]&0x0F, uint64(head[1]&0x7F)
		if n == 126 {
			b := make([]byte, 2)
			if _, err := io.ReadFull(w.r, b); err != nil {
				return "", err
			}
			n = uint64(binary.BigEndian.Uint16(b))
		} else if n == 127 {
			b := make([]byte, 8)
			if _, err := io.ReadFull(w.r, b); err != nil {
				return "", err
			}
			n = binary.BigEndian.Uint64(b)
		}
		if head[1]&0x80 != 0 { // servers don't mask; skip a mask if one is sent
			io.ReadFull(w.r, make([]byte, 4))
		}
		if n > maxBrowserMessage || uint64(len(message))+n > maxBrowserMessage {
			return "", errors.New("the browser sent a message that is too large")
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(w.r, payload); err != nil {
			return "", errors.New("the browser closed the connection")
		}
		switch opcode {
		case 0x8:
			return "", errors.New("the browser closed the connection")
		case 0x9:
			w.sendFrame(0xA, payload) // ping -> pong
			continue
		case 0xA:
			continue
		}
		message = append(message, payload...)
		if fin {
			return string(message), nil
		}
	}
}

func (w *wsConn) close() { w.c.Close() }

// cdp speaks the Chrome DevTools Protocol to one target (a page, or the browser itself).
type cdp struct {
	ws      *wsConn
	nextID  int
	timeout time.Duration
}

func dialCDP(wsURL string, timeout time.Duration) (*cdp, error) {
	ws, err := wsDial(wsURL, timeout)
	if err != nil {
		return nil, err
	}
	return &cdp{ws: ws, timeout: timeout}, nil
}

func (c *cdp) call(method string, params map[string]any) (map[string]any, error) {
	c.nextID++
	if params == nil {
		params = map[string]any{}
	}
	msg, _ := json.Marshal(map[string]any{"id": c.nextID, "method": method, "params": params})
	c.ws.c.SetDeadline(time.Now().Add(c.timeout))
	if err := c.ws.send(string(msg)); err != nil {
		return nil, err
	}
	for {
		text, err := c.ws.recv()
		if err != nil {
			return nil, err
		}
		var reply struct {
			ID     int            `json:"id"`
			Result map[string]any `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(text), &reply) != nil || reply.ID != c.nextID {
			continue // anything else is an event we did not ask for
		}
		if reply.Error != nil {
			m := reply.Error.Message
			if m == "" {
				m = "browser error"
			}
			return nil, errors.New(m)
		}
		return reply.Result, nil
	}
}

func (c *cdp) evaluate(expression string) (any, error) {
	r, err := c.call("Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": false})
	if err != nil {
		return nil, err
	}
	if ex, ok := r["exceptionDetails"].(map[string]any); ok {
		desc := "the page script failed"
		if e, ok := ex["exception"].(map[string]any); ok {
			if d, ok := e["description"].(string); ok && d != "" {
				desc = d
			}
		}
		return nil, errors.New(desc)
	}
	res, _ := r["result"].(map[string]any)
	return res["value"], nil
}

// evaluateJSON runs an expression that returns JSON text and decodes it into out.
func (c *cdp) evaluateJSON(expression string, out any) error {
	v, err := c.evaluate(expression)
	if err != nil {
		return err
	}
	s, ok := v.(string)
	if !ok {
		return errors.New("the page answered something unexpected")
	}
	return json.Unmarshal([]byte(s), out)
}

func (c *cdp) close() { c.ws.close() }

// errNotSignedIn: the RSI window isn't open or signed in.
type notSignedIn struct{ msg string }

func (e notSignedIn) Error() string { return e.msg }

type pageInfo struct {
	Host   string `json:"host"`
	Path   string `json:"path"`
	Signed bool   `json:"signed"`
}

type rsiWindow struct {
	mu   sync.Mutex
	proc *exec.Cmd
	done chan struct{}
	port int
}

var window = &rsiWindow{}

var localHTTP = &http.Client{Timeout: 5 * time.Second}

// browserJSON talks to the browser's HTTP side (/json, /json/version...).
func (w *rsiWindow) browserJSON(path, method string) (any, error) {
	req, _ := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", w.port, path), nil)
	resp, err := localHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(body)) == "" {
		return nil, nil
	}
	var v any
	if json.Unmarshal(body, &v) != nil {
		return string(body), nil // e.g. "Target activated"
	}
	return v, nil
}

func (w *rsiWindow) readPortFile() (int, bool) {
	b, err := os.ReadFile(filepath.Join(profileDir(), "DevToolsActivePort"))
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(fields[0])
	return n, err == nil
}

// adopt reconnects to a window Hangar opened earlier, e.g. before Hangar itself was restarted.
func (w *rsiWindow) adopt() {
	port, ok := w.readPortFile()
	if !ok {
		return
	}
	w.port = port
	if _, err := w.browserJSON("/json/version", "GET"); err != nil {
		w.port = 0
	}
}

func (w *rsiWindow) exited() bool {
	if w.done == nil {
		return false
	}
	select {
	case <-w.done:
		return true
	default:
		return false
	}
}

func (w *rsiWindow) alive() bool {
	if w.port == 0 && w.proc == nil {
		w.adopt()
	}
	if w.port == 0 || (w.proc != nil && w.exited()) {
		return false
	}
	_, err := w.browserJSON("/json/version", "GET")
	return err == nil
}

type target struct {
	ID    string
	URL   string
	WSURL string
}

func (w *rsiWindow) pages() []target {
	v, err := w.browserJSON("/json", "GET")
	if err != nil {
		return nil
	}
	list, _ := v.([]any)
	out := []target{}
	for _, x := range list {
		t, _ := x.(map[string]any)
		if t["type"] == "page" {
			out = append(out, target{str(t["id"]), str(t["url"]), str(t["webSocketDebuggerUrl"])})
		}
	}
	return out
}

func rsiHost() string {
	u, _ := url.Parse(rsiURL())
	return u.Hostname()
}

func (w *rsiWindow) target() *target {
	pages := w.pages()
	for _, t := range pages {
		if u, err := url.Parse(t.URL); err == nil && u.Hostname() == rsiHost() {
			return &t
		}
	}
	if len(pages) > 0 {
		return &pages[0]
	}
	return nil
}

// open opens (or brings forward) the Hangar RSI window on the pledges page and returns the browser's name.
func (w *rsiWindow) open() (string, error) {
	found := findBrowser()
	if found == nil {
		return "", userErr("No Chrome, Edge or Brave browser was found on this computer.")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.alive() {
		if t := w.target(); t != nil {
			w.browserJSON("/json/activate/"+t.ID, "GET")
		} else {
			w.browserJSON("/json/new?"+rsiURL(), "PUT")
		}
		return found.Name, nil
	}
	os.MkdirAll(profileDir(), 0o700)
	portFile := filepath.Join(profileDir(), "DevToolsActivePort")
	os.Remove(portFile)
	args := []string{"--user-data-dir=" + profileDir(), "--remote-debugging-port=0", "--no-first-run",
		"--no-default-browser-check", "--new-window"}
	if os.Getenv("HANGAR_BROWSER_HEADLESS") != "" {
		args = append(args, "--headless=new")
	}
	cmd := exec.Command(found.Path, append(args, rsiURL())...)
	if err := cmd.Start(); err != nil {
		return "", userErr("The browser could not be started: %v", err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	w.proc, w.done = cmd, done
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if port, ok := w.readPortFile(); ok {
			w.port = port
			return found.Name, nil
		}
		if w.exited() {
			w.proc, w.done = nil, nil
			return "", userErr("The browser closed straight away. Is another window using the Hangar profile?")
		}
		time.Sleep(200 * time.Millisecond)
	}
	return "", userErr("The browser did not start in time.")
}

// close closes the window gracefully (so the sign-in is saved), then makes sure the process is gone.
func (w *rsiWindow) close() {
	if w.alive() {
		if v, err := w.browserJSON("/json/version", "GET"); err == nil {
			m, _ := v.(map[string]any)
			if wsURL := str(m["webSocketDebuggerUrl"]); wsURL != "" {
				if b, err := dialCDP(wsURL, 5*time.Second); err == nil {
					b.call("Browser.close", nil)
					b.close()
				}
			}
		}
	}
	deadline := time.Now().Add(8 * time.Second) // wait until it has really gone, also for a window Hangar did not start
	for w.port != 0 && time.Now().Before(deadline) {
		if _, err := w.browserJSON("/json/version", "GET"); err != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if w.proc != nil {
		select {
		case <-w.done:
		case <-time.After(8 * time.Second):
			w.proc.Process.Kill()
		}
		w.proc, w.done = nil, nil
	}
	w.port = 0
}

// forget closes the window and deletes the saved RSI sign-in.
func (w *rsiWindow) forget() {
	w.close()
	os.RemoveAll(profileDir())
}

// status says whether the window is open, on RSI, and signed in.
func (w *rsiWindow) status() map[string]any {
	if !w.alive() {
		return map[string]any{"open": false}
	}
	t := w.target()
	if t == nil {
		return map[string]any{"open": true, "on_rsi": false, "signed_in": false}
	}
	c, err := dialCDP(t.WSURL, 10*time.Second)
	if err != nil {
		return map[string]any{"open": true, "on_rsi": false, "signed_in": false}
	}
	defer c.close()
	var info pageInfo
	if c.evaluateJSON(checkJS, &info) != nil {
		return map[string]any{"open": true, "on_rsi": false, "signed_in": false}
	}
	onRSI := info.Host == rsiHost()
	return map[string]any{"open": true, "on_rsi": onRSI, "signed_in": onRSI && info.Signed}
}

type rsiRead struct {
	Pledges  any `json:"pledges"`
	Upgrades any `json:"upgrades"`
	Events   any `json:"events"`
	LogNote  any `json:"logNote"`
}

// read reads the pledges and the Hangar log from the window; progress(stage, current, total) is called as pages are read.
func (w *rsiWindow) read(progress func(stage string, current, total int)) (*rsiRead, error) {
	if !w.alive() {
		return nil, notSignedIn{"The RSI window is not open."}
	}
	t := w.target()
	if t == nil {
		return nil, notSignedIn{"The RSI window has no page open."}
	}
	c, err := dialCDP(t.WSURL, 60*time.Second)
	if err != nil {
		return nil, err
	}
	defer c.close()
	var info pageInfo
	if err := c.evaluateJSON(checkJS, &info); err != nil {
		return nil, err
	}
	want := strings.TrimRight(mustURL(rsiURL()).Path, "/")
	onPledges := func() bool { return info.Host == rsiHost() && strings.TrimRight(info.Path, "/") == want }
	if !onPledges() {
		c.call("Page.navigate", map[string]any{"url": rsiURL()}) // signed in but sitting on another RSI page
		for i := 0; i < 30; i++ {
			time.Sleep(500 * time.Millisecond)
			if c.evaluateJSON(checkJS, &info) != nil {
				continue
			}
			if onPledges() && info.Signed {
				break
			}
		}
	}
	if !info.Signed || info.Host != rsiHost() {
		return nil, notSignedIn{"Hangar's RSI window isn't signed in yet. Sign in there, then click Continue."}
	}
	reader, err := webFiles.ReadFile("web/rsi_read.js")
	if err != nil {
		return nil, err
	}
	script := string(reader) + `
window.__hangarOut = null; window.__hangarProgress = null;
(async function () {
  try {
    var r = await hangarRead(function (s, c, t) { window.__hangarProgress = [s, c, t]; });
    window.__hangarOut = JSON.stringify({ok: true, data: r});
  } catch (e) {
    window.__hangarOut = JSON.stringify({ok: false, error: String((e && e.message) || e)});
  }
})();
`
	if _, err := c.evaluate(script); err != nil { // starts reading; the page keeps going while we watch
		return nil, err
	}
	deadline := time.Now().Add(600 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(800 * time.Millisecond)
		var state []json.RawMessage
		if err := c.evaluateJSON("JSON.stringify([window.__hangarProgress, window.__hangarOut != null])", &state); err != nil {
			return nil, err
		}
		if len(state) == 2 {
			var p []any
			if json.Unmarshal(state[0], &p) == nil && len(p) == 3 {
				cur, _ := p[1].(float64)
				tot, _ := p[2].(float64)
				progress(str(p[0]), int(cur), int(tot))
			}
			if string(state[1]) == "true" {
				var out struct {
					OK    bool    `json:"ok"`
					Error string  `json:"error"`
					Data  rsiRead `json:"data"`
				}
				v, err := c.evaluate("window.__hangarOut")
				if err != nil {
					return nil, err
				}
				dec := json.NewDecoder(strings.NewReader(str(v)))
				dec.UseNumber()
				if err := dec.Decode(&out); err != nil {
					return nil, err
				}
				if !out.OK {
					return nil, notSignedIn{out.Error}
				}
				return &out.Data, nil
			}
		}
	}
	return nil, userErr("Reading RSI took too long. Try again.")
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		return &url.URL{}
	}
	return u
}
