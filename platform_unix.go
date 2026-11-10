//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func isExecutableMode(st os.FileInfo) bool { return st.Mode()&0o111 != 0 }

func ownedByMe(folder string) bool {
	st, err := os.Stat(folder)
	if err != nil {
		return false
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	return ok && int(sys.Uid) == os.Getuid()
}

// restartSelf replaces this process with a fresh copy of the program.
func restartSelf(exe string, args, env []string) error {
	return syscall.Exec(exe, args, env)
}

func hideWindow(cmd *exec.Cmd) {}
