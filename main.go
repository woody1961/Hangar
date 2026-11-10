// Hangar: a desktop app for your Star Citizen hangar, written in Go, with its SQLite database built in.
package main

import (
	"embed"
	"flag"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gioui.org/f32"
)

// Only the two scripts that run inside RSI's own pages (the reader and the bookmark) are not Go: they have to run in
// the browser, on RSI's site.
//
//go:embed web/rsi_read.js web/rsi_import.js
var webFiles embed.FS

// version is set when the release programs are built.
var version = "dev"

// baseDir is where settings.json (and, by default, the data folder) live.
var baseDir string

// fatalStart is set when Hangar can't open its data; the window then explains instead of showing the hangar.
var fatalStart string

const osSep = os.PathSeparator

type rgbaColor = color.NRGBA

func f32pt(x, y float32) f32.Point { return f32.Pt(x, y) }

func baseName(p string) string { return filepath.Base(p) }

// programFolder is the folder holding the program (for a Mac app, the folder holding Hangar.app).
func programFolder() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	dir := filepath.Dir(exe)
	if runtime.GOOS == "darwin" && strings.HasSuffix(dir, ".app/Contents/MacOS") {
		dir = filepath.Dir(filepath.Dir(filepath.Dir(dir)))
	}
	return dir
}

// chooseBaseDir: HANGAR_HOME wins; then the program's own folder when it already holds Hangar's settings or data
// (a "portable" copy, or the earlier Python Hangar's folder); otherwise this person's application-data folder.
func chooseBaseDir() string {
	if d := os.Getenv("HANGAR_HOME"); d != "" {
		return absPath(d)
	}
	prog := programFolder()
	if fileExists(filepath.Join(prog, "settings.json")) || fileExists(filepath.Join(prog, "data", "ships.db")) {
		return prog
	}
	if cfg, err := os.UserConfigDir(); err == nil {
		dir := filepath.Join(cfg, "Hangar")
		if os.MkdirAll(dir, 0o755) == nil {
			return dir
		}
	}
	return prog
}

func main() {
	showVersion := flag.Bool("version", false, "print the version and quit")
	flag.Parse()
	if *showVersion {
		fmt.Println("Hangar", version)
		return
	}
	baseDir = chooseBaseDir()
	setDataDir(chooseDataDir())
	setPort(configuredPort())
	dailyBackup()
	conn, err := connect()
	if err != nil {
		fatalStart = fmt.Sprintf("Hangar couldn't open its database in %s:\n%v", dataDir, err)
	} else {
		store = conn
	}
	runWindow()
}

// restartApp starts Hangar again in place, e.g. after the data folder or the port changed.
func (a *App) restartApp() {
	window.close()
	if receiver != nil {
		receiver.Close()
	}
	dbMu.Lock()
	if store != nil {
		store.Close()
	}
	exe, err := os.Executable()
	if err == nil {
		err = restartSelf(exe, os.Args, os.Environ())
	}
	dbMu.Unlock()
	a.fatal = "Hangar couldn't restart itself. Close it and open it again. (" + fmt.Sprint(err) + ")"
}

// systemIsDark asks the computer whether it is in dark mode, for the "System" theme.
func systemIsDark() bool {
	run := func(name string, args ...string) string {
		cmd := exec.Command(name, args...)
		hideWindow(cmd)
		done := make(chan []byte, 1)
		go func() { out, _ := cmd.Output(); done <- out }()
		select {
		case out := <-done:
			return string(out)
		case <-time.After(2 * time.Second):
			return ""
		}
	}
	switch runtime.GOOS {
	case "darwin":
		return strings.Contains(run("defaults", "read", "-g", "AppleInterfaceStyle"), "Dark")
	case "windows":
		out := run("reg", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "/v", "AppsUseLightTheme")
		return strings.Contains(out, "0x0")
	default:
		out := run("gsettings", "get", "org.gnome.desktop.interface", "color-scheme")
		return strings.Contains(out, "dark")
	}
}

// downloadsPath is where an export goes when the computer offers no save dialog: Downloads, without overwriting.
func downloadsPath(name string) string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Downloads")
	if !isDir(dir) {
		dir = home
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	p := filepath.Join(dir, name)
	for n := 2; fileExists(p); n++ {
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, n, ext))
	}
	return p
}

// revealFile shows a saved file in the computer's file manager.
func revealFile(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	case "windows":
		cmd = exec.Command("explorer", "/select,", path)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	}
	if cmd.Start() == nil {
		go cmd.Wait()
	}
}
