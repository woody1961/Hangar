package main

// Who else has this data folder open. A data folder on a shared drive can be opened from two computers, and two
// copies of Hangar changing one database at the same time can damage it. Each Hangar keeps a small note in the data
// folder saying which computer has it open, refreshed every minute; a note from another computer or window that is
// less than three minutes old means it is open there too, and the window says so.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	inUseName  = ".hangar-open"
	inUseEvery = time.Minute
	inUseFresh = 3 * time.Minute
)

type inUseNote struct {
	Host string    `json:"host"`
	PID  int       `json:"pid"`
	Time time.Time `json:"time"`
}

var (
	inUseMu    sync.Mutex
	inUseOther string // who else has the folder open ("" when nobody)
	inUseStop  = make(chan struct{})
	myHost, _  = os.Hostname()
)

func inUsePath() string { return filepath.Join(dataDir, inUseName) }

// readOtherUser returns who else has the folder open, judging by the note in it.
func readOtherUser() string {
	data, err := os.ReadFile(inUsePath())
	if err != nil {
		return ""
	}
	var n inUseNote
	if json.Unmarshal(data, &n) != nil || time.Since(n.Time) > inUseFresh || time.Until(n.Time) > inUseFresh {
		return ""
	}
	switch {
	case n.Host == myHost && n.PID == os.Getpid():
		return ""
	case n.Host == myHost:
		return "another Hangar window on this computer"
	case n.Host == "":
		return "another computer"
	}
	return n.Host
}

func writeInUse() {
	data, _ := json.Marshal(inUseNote{Host: myHost, PID: os.Getpid(), Time: time.Now().UTC()})
	os.WriteFile(inUsePath(), data, 0o600) // a read-only folder just means no note
}

// watchInUse notes that this Hangar has the folder open and keeps checking whether another one has too; changed is
// called when that answer changes.
func watchInUse(changed func()) {
	check := func() {
		other := readOtherUser()
		inUseMu.Lock()
		was := inUseOther
		inUseOther = other
		inUseMu.Unlock()
		if other == "" {
			writeInUse() // the other one keeps its own note fresh; ours goes in once it has gone
		}
		if other != was && changed != nil {
			changed()
		}
	}
	check()
	go func() {
		t := time.NewTicker(inUseEvery)
		defer t.Stop()
		for {
			select {
			case <-inUseStop:
				return
			case <-t.C:
				check()
			}
		}
	}()
}

// releaseInUse removes this Hangar's note when it closes (only if the note is still its own).
func releaseInUse() {
	select {
	case <-inUseStop:
	default:
		close(inUseStop)
	}
	data, err := os.ReadFile(inUsePath())
	if err != nil {
		return
	}
	var n inUseNote
	if json.Unmarshal(data, &n) == nil && n.Host == myHost && n.PID == os.Getpid() {
		os.Remove(inUsePath())
	}
}

func otherUser() string {
	inUseMu.Lock()
	defer inUseMu.Unlock()
	return inUseOther
}

func inUseWarning(who string) string {
	return fmt.Sprintf("This data folder is also open in Hangar on %s. Close Hangar there before changing anything here: "+
		"two copies changing one database at the same time can damage it.", who)
}
