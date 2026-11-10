//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func isExecutableMode(st os.FileInfo) bool { return true }

func ownedByMe(folder string) bool { return false }

// restartSelf starts a fresh copy of the program in this window, then ends this one.
func restartSelf(exe string, args, env []string) error {
	cmd := exec.Command(exe, args[1:]...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// hideWindow keeps a helper command (like reading the dark-mode setting) from flashing a console window.
func hideWindow(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
