package main

// Hangar's settings live in a small settings.json next to the program (not inside the data folder, because the data
// folder is itself a setting). HANGAR_SETTINGS keeps the file somewhere else. The environment variables SHIPS_DATA,
// SHIPS_PORT and HANGAR_BROWSER still win over the file, and the settings screen shows when one of them is in charge.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// The columns of the item table that can be hidden ("name" always stays).
var columnKeys = []string{"name", "type", "manufacturer", "purpose", "insurance", "crew", "storage_scu", "flight_ready",
	"pledge_date", "purchase_value", "value", "pledge_id", "membership_id"}

var settingDefaults = map[string]any{
	// appearance
	"theme":          "system",      // system | light | dark
	"font_size":      "default",     // small | default | large
	"thumb_size":     "medium",      // small 54x33 | medium 108x66 | large 216x133
	"density":        "comfortable", // comfortable | compact
	"hidden_columns": []string{},    // keys from columnKeys
	"date_format":    "iso",         // iso 2026-10-14 | long Oct 14, 2026
	// privacy
	"hide_amounts": false,
	"hide_ids":     false,
	// startup
	"open_browser": true,
	"start_view":   "items", // items | packages
	"port":         8742,    // needs a restart
	// RSI import
	"rsi_close_after_import": false,
	"rsi_browser":            "auto", // auto, or the path of a Chrome/Edge/Brave
	"download_pictures":      true,
	// data
	"backups_daily":     14,
	"backups_snapshots": 5,
	"data_dir":          nil, // set by "Move data to…" only; needs a restart
	"previous_data_dir": nil, // the folder left behind by a move, until you delete it
}

var settingChoices = map[string][]string{
	"theme":       {"system", "light", "dark"},
	"font_size":   {"small", "default", "large"},
	"thumb_size":  {"small", "medium", "large"},
	"density":     {"comfortable", "compact"},
	"date_format": {"iso", "long"},
	"start_view":  {"items", "packages"},
}

var settingBools = []string{"hide_amounts", "hide_ids", "open_browser", "rsi_close_after_import", "download_pictures"}
var settingInts = map[string][2]int{"port": {1024, 65535}, "backups_daily": {1, 365}, "backups_snapshots": {1, 100}}
var settingInternal = []string{"data_dir", "previous_data_dir"} // changed by the data-folder move, not by hand
var envOverrides = map[string]string{"data_dir": "SHIPS_DATA", "port": "SHIPS_PORT", "rsi_browser": "HANGAR_BROWSER"}

var settingsMu sync.Mutex

func settingsPath() string {
	if p := os.Getenv("HANGAR_SETTINGS"); p != "" {
		return p
	}
	return filepath.Join(baseDir, "settings.json")
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// checkSetting returns the cleaned value or an error. Values come from encoding/json with UseNumber, so whole numbers
// can be told apart from 8742.5 or "8742".
func checkSetting(key string, value any, strict bool) (any, error) {
	if choices, ok := settingChoices[key]; ok {
		s, isStr := value.(string)
		if !isStr || !contains(choices, s) {
			return nil, userErr("%s must be one of: %s", key, strings.Join(choices, ", "))
		}
		return s, nil
	}
	if contains(settingBools, key) {
		b, ok := value.(bool)
		if !ok {
			return nil, userErr("%s must be true or false", key)
		}
		return b, nil
	}
	if r, ok := settingInts[key]; ok {
		n, ok := wholeNumber(value)
		if !ok || n < r[0] || n > r[1] {
			return nil, userErr("%s must be a whole number from %d to %d", key, r[0], r[1])
		}
		return n, nil
	}
	switch key {
	case "hidden_columns":
		list, ok := value.([]any)
		if !ok {
			if l, ok2 := value.([]string); ok2 {
				for _, s := range l {
					list = append(list, s)
				}
				ok = true
			}
		}
		if !ok {
			return nil, userErr("hidden_columns must be a list of column names (the Name column can't be hidden)")
		}
		out := []string{}
		for _, v := range list {
			s, isStr := v.(string)
			if !isStr || !contains(columnKeys, s) || s == "name" {
				return nil, userErr("hidden_columns must be a list of column names (the Name column can't be hidden)")
			}
			if !contains(out, s) {
				out = append(out, s)
			}
		}
		return out, nil
	case "rsi_browser":
		s, ok := value.(string)
		if !ok || s == "" {
			return nil, userErr("rsi_browser must be 'auto' or the path of a browser")
		}
		if strict && s != "auto" && !isExecutable(s) {
			return nil, userErr("That browser could not be found")
		}
		return s, nil
	case "data_dir", "previous_data_dir":
		if value == nil {
			return nil, nil
		}
		s, ok := value.(string)
		if !ok {
			return nil, userErr("%s must be a path", key)
		}
		return s, nil
	}
	return nil, userErr("Unknown setting: %s", key)
}

func wholeNumber(v any) (int, bool) {
	switch n := v.(type) {
	case json.Number:
		if strings.ContainsAny(n.String(), ".eE") {
			return 0, false
		}
		i, err := n.Int64()
		return int(i), err == nil
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}

func copyDefaults() map[string]any {
	out := map[string]any{}
	for k, v := range settingDefaults {
		if l, ok := v.([]string); ok {
			v = append([]string{}, l...)
		}
		out[k] = v
	}
	return out
}

// settingsValues returns every setting, with defaults filled in. A bad value in the file falls back to its default.
func settingsValues() map[string]any {
	out := copyDefaults()
	raw, err := os.ReadFile(settingsPath())
	if err != nil {
		return out
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var file map[string]any
	if dec.Decode(&file) != nil {
		return out
	}
	for k, v := range file {
		if _, known := settingDefaults[k]; !known {
			continue
		}
		if clean, err := checkSetting(k, v, false); err == nil {
			out[k] = clean
		}
	}
	return out
}

func settingString(key string) string {
	s, _ := settingsValues()[key].(string)
	return s
}

func settingInt(key string) int {
	n, _ := settingsValues()[key].(int)
	return n
}

func settingBool(key string) bool {
	b, _ := settingsValues()[key].(bool)
	return b
}

// writeSettings never leaves a half-written file: it writes beside it and renames.
func writeSettings(data map[string]any) error {
	path := settingsPath()
	folder := filepath.Dir(path)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return err
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, k := range keys {
		v, _ := json.Marshal(data[k])
		fmt.Fprintf(&buf, "  %q: %s", k, v)
		if i < len(keys)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}")
	tmp, err := os.CreateTemp(folder, ".settings-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// updateSettings applies changes from the settings screen (validated, all or nothing).
func updateSettings(changes map[string]any) (map[string]any, error) {
	clean := map[string]any{}
	for k, v := range changes {
		if contains(settingInternal, k) {
			return nil, userErr("%s can't be changed here", k)
		}
		c, err := checkSetting(k, v, true)
		if err != nil {
			return nil, err
		}
		clean[k] = c
	}
	settingsMu.Lock()
	defer settingsMu.Unlock()
	merged := settingsValues()
	for k, v := range clean {
		merged[k] = v
	}
	return merged, writeSettings(merged)
}

// setInternalSettings is for the data-folder move: it changes the keys updateSettings refuses.
func setInternalSettings(changes map[string]any) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	merged := settingsValues()
	for k, v := range changes {
		if !contains(settingInternal, k) {
			return fmt.Errorf("%s is not an internal setting", k)
		}
		c, err := checkSetting(k, v, false)
		if err != nil {
			return err
		}
		merged[k] = c
	}
	return writeSettings(merged)
}

// activeEnvOverrides lists {setting: environment variable} for the settings an environment variable decides now.
func activeEnvOverrides() map[string]string {
	out := map[string]string{}
	for k, v := range envOverrides {
		if os.Getenv(v) != "" {
			out[k] = v
		}
	}
	return out
}

func isExecutable(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	return isExecutableMode(st)
}
