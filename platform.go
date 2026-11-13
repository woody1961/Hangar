package main

// Things that differ between macOS, Windows and Linux: opening a folder or a web page, and asking for a folder.

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
	go cmd.Wait()
}

func openFolder(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("explorer", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return userErr("The folder couldn't be opened: %v", err)
	}
	go cmd.Wait()
	return nil
}

// pickFolder asks the operating system for a folder with its own dialog. "" means cancelled.
func pickFolder() (string, error) {
	const prompt = "Choose the folder for Hangar's data"
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("osascript", "-e", `tell application "System Events" to activate`,
			"-e", `POSIX path of (choose folder with prompt "`+prompt+`")`)
	case "windows":
		script := "Add-Type -AssemblyName System.Windows.Forms; $d = New-Object System.Windows.Forms.FolderBrowserDialog; " +
			"$d.Description = '" + prompt + "'; if ($d.ShowDialog() -eq 'OK') { $d.SelectedPath }"
		cmd = exec.Command("powershell", "-NoProfile", "-STA", "-Command", script)
	default:
		if p, err := exec.LookPath("zenity"); err == nil {
			cmd = exec.Command(p, "--file-selection", "--directory", "--title="+prompt)
		} else if p, err := exec.LookPath("kdialog"); err == nil {
			home, _ := os.UserHomeDir()
			cmd = exec.Command(p, "--getexistingdirectory", home)
		} else {
			return "", userErr("This computer has no folder picker Hangar can use. Type the folder instead.")
		}
	}
	out, err := cmd.Output()
	if err != nil {
		if _, isExit := err.(*exec.ExitError); isExit {
			return "", nil // the dialog was cancelled
		}
		return "", userErr("The folder picker couldn't be opened. Type the folder instead.")
	}
	path := strings.TrimSpace(string(out))
	if trimmed := strings.TrimRight(path, `/\`); trimmed != "" {
		path = trimmed
	}
	return path, nil
}
