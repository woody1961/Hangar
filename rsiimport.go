package main

// Merge what Hangar reads from your RSI account into the database.
//
// Both ways of importing (Hangar's own RSI window and the bookmark) hand over the same:
//   pledges:  [{"id", "name", "value", "created", "gift_pending", "items": [{"title", "kind", "manufacturer", "code", "image"}],
//              "nameable": [{"membership_id", "custom_name", "default_name"}]}]   (your pledges page)
//   upgrades: lines of your Hangar log that say an upgrade was applied
//   events:   lines of your Hangar log that say what happened to a pledge (created, melted, gifted, ...)
//   complete: true only when every page of the hangar AND the log was read (a missing pledge is then really gone)
//
// Packages: a pledge already in the database is skipped (only checked for missing pictures); otherwise it claims a
// package you added by hand with the same name; otherwise it becomes a new package with all its items.
// Upgrades: the ship is renamed to what RSI shows today, keeping the chain in items.upgrade_history.
// Insurance belongs to the ships; where a ship could have several terms the longest applies.
// Pledges that left the hangar are marked melted / gifted / missing (never deleted), only when complete is true.
// Every import first saves a copy of the database (backups/before-rsi-import-*.db), and is all or nothing.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	maxUnmatchedReported = 50
	maxUpgradeDetails    = 40
)

var nonVehicleTypes = map[string]bool{"skin": true, "flair": true, "gear": true, "clothing": true, "fps equipment": true,
	"game": true, "hanger": true, "hangar": true, "insurance": true}

var (
	upgradeRe = regexp.MustCompile(`^(?P<date>[A-Z][a-z]{2} \d{1,2} \d{4}), (?P<time>\d{1,2}:\d{2} [ap]m) - (?P<target>.+?) #(?P<target_id>\d+) - ` +
		`Upgrade applied: #(?P<upgrade_id>\d+) (?:Ship Upgrades|Upgrade) - (?P<from>.+?) [Tt]o (?P<to>.+?), ` +
		`new value: (?P<value>\$[\d,.]+(?: [A-Z]{3})?)`)
	lifecycleRe = regexp.MustCompile(`^(?P<date>[A-Z][a-z]{2} \d{1,2} \d{4}), (?P<time>\d{1,2}:\d{2} [ap]m) - (?P<name>.+?) #(?P<id>\d+) - ` +
		`(?:(?P<melt>Reclaimed by (?P<mby>.+?) for (?P<credit>\$[\d,.]+(?: [A-Z]{3})?))` +
		`|(?P<gift>Gifted to (?P<to>.+?), value: (?P<gvalue>\$[\d,.]+(?: [A-Z]{3})?))` +
		`|(?P<cancelled>Gift cancelled by (?P<kby>.+?), value: (?P<kvalue>\$[\d,.]+(?: [A-Z]{3})?))` +
		`|(?P<config>Configuration reclaimed by (?P<cfby>.+?) for (?P<ccredit>\$[\d,.]+(?: [A-Z]{3})?))` +
		`|(?P<created>Created by (?P<cby>[^,]+?)(?: - order #\w+)?, value: (?P<cvalue>\$[\d,.]+(?: [A-Z]{3})?))` +
		`|(?P<consumed>Consumed by (?P<xby>.+?) on pledge #(?P<target>\d+), value: (?P<consvalue>\$[\d,.]+(?: [A-Z]{3})?))` +
		`|(?P<received>Claimed as a gift by (?P<rby>.+?), value: (?P<rvalue>\$[\d,.]+(?: [A-Z]{3})?)))`)
	markerRe      = regexp.MustCompile(`(?i)\s*[\(\{]\s*upgraded?\s*[\)\}]`)
	nonAlnumRe    = regexp.MustCompile(`[^a-z0-9]+`)
	createdRe     = regexp.MustCompile(`([A-Z][a-z]+ \d{1,2}, \d{4})`)
	pledgeIDRe    = regexp.MustCompile(`^\d{1,12}$`)
	membershipRe  = regexp.MustCompile(`^\d{1,15}$`)
	editionRe     = regexp.MustCompile(`(?i)\s+(?:Standard|Warbond|Limited|Subscriber|Anniversary)?\s*(?:Edition|Upgrade)\s*$`)
	bareEditionRe = regexp.MustCompile(`(?i)^(?:Standard|Warbond|Limited|Subscriber|Anniversary)$`)
	moneyRe       = regexp.MustCompile(`\$\s*([\d,]+(?:\.\d+)?)`)
	lifetimeRe    = regexp.MustCompile(`(?i)\blifetime\b`)
	termRe        = regexp.MustCompile(`(?i)(\d+)\s*[- ]?\s*(month|year)s?`)
	insuranceRe   = regexp.MustCompile(`(?i)\s*insurance\s*$`)
	rankRe        = regexp.MustCompile(`^(\d+) (month|year)`)
)

// ---------- text helpers ----------

// normText: "A.T.L.S" -> "a t l s", accents dropped, lower case, only letters and digits.
func normText(s string) string {
	s = norm.NFKD.String(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(nonAlnumRe.ReplaceAllString(strings.ToLower(b.String()), " "))
}

// core is the normalised ship name without hand-written "(Upgraded)" markers.
func core(s string) string { return normText(markerRe.ReplaceAllString(s, " ")) }

// matchLevel: 2 = same name ignoring spacing/punctuation, 1 = one name's words are all in the other.
func matchLevel(a, b string) int {
	ca, cb := core(a), core(b)
	if ca == "" || cb == "" {
		return 0
	}
	if strings.ReplaceAll(ca, " ", "") == strings.ReplaceAll(cb, " ", "") {
		return 2
	}
	ta, tb := wordSet(ca), wordSet(cb)
	if subset(ta, tb) || subset(tb, ta) {
		return 1
	}
	return 0
}

func wordSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(s) {
		out[w] = true
	}
	return out
}

func subset(a, b map[string]bool) bool {
	for w := range a {
		if !b[w] {
			return false
		}
	}
	return true
}

// cut keeps the first n characters, as Python's s[:n] does.
func cut(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// pyText is str(v or "") for a JSON value: empty for anything falsy.
func pyText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "True"
		}
		return ""
	case json.Number:
		if f, err := x.Float64(); err == nil && f == 0 {
			return ""
		}
		if i, err := x.Int64(); err == nil {
			return strconv.FormatInt(i, 10)
		}
		if f, err := x.Float64(); err == nil {
			return pyFloat(f)
		}
		return x.String()
	case int64:
		if x == 0 {
			return ""
		}
		return strconv.FormatInt(x, 10)
	case float64:
		if x == 0 {
			return ""
		}
		return pyFloat(x)
	case []any:
		if len(x) == 0 {
			return ""
		}
	case map[string]any:
		if len(x) == 0 {
			return ""
		}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// pyFloat writes a number the way Python's str(float) does: 25.0, 12.5.
func pyFloat(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e16 {
		return strconv.FormatFloat(f, 'f', 1, 64)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// money: "$1,045.00 USD" -> 1045 (ok false when there is no amount).
func money(text string) (float64, bool) {
	m := moneyRe.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	return f, err == nil
}

func parseCreated(text string) any {
	m := createdRe.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	t, err := time.Parse("January 2, 2006", m[1])
	if err != nil {
		return nil
	}
	return t.Format("2006-01-02")
}

// logTime reads "Sep 26 2025" + "5:28 pm" as "2025-09-26T17:28".
func logTime(date, clock string) (string, bool) {
	t, err := time.Parse("Jan 2 2006 3:04 pm", date+" "+clock)
	if err != nil {
		return "", false
	}
	return t.Format("2006-01-02T15:04"), true
}

func isRSIImage(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return u.Scheme == "https" && (host == "robertsspaceindustries.com" || strings.HasSuffix(host, ".robertsspaceindustries.com"))
}

func named(re *regexp.Regexp, m []string, name string) string {
	return m[re.SubexpIndex(name)]
}

// ---------- what the browser sent ----------

type rsiItem struct {
	CustomName, MembershipID, Title, Kind, Manufacturer, Code, Image string
}

type pledge struct {
	ID, Name, Value, Created string
	GiftPending              bool
	Items                    []*rsiItem
}

func isInsurance(i *rsiItem) bool { return strings.ToLower(i.Kind) == "insurance" }

// cleanPledges validates and trims what the browser sent.
func cleanPledges(raw any) ([]*pledge, error) {
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return nil, userErr("No pledges found in the data")
	}
	out := []*pledge{}
	seen := map[string]bool{}
	for _, entry := range list {
		p, ok := entry.(map[string]any)
		if !ok {
			return nil, userErr("Unexpected data format")
		}
		pid := strings.TrimSpace(pyText(p["id"]))
		name := cut(strings.TrimSpace(pyText(p["name"])), 300)
		if !pledgeIDRe.MatchString(pid) || name == "" || seen[pid] {
			continue // a pledge listed twice (a page shifted while it was being read) counts once
		}
		seen[pid] = true
		items := []*rsiItem{}
		rawItems, _ := p["items"].([]any)
		for _, x := range rawItems {
			it, ok := x.(map[string]any)
			if !ok {
				continue
			}
			title := cut(strings.TrimSpace(pyText(it["title"])), 300)
			if title == "" {
				continue
			}
			image, _ := it["image"].(string)
			if !isRSIImage(image) {
				image = ""
			}
			items = append(items, &rsiItem{
				CustomName:   cut(strings.TrimSpace(pyText(it["custom_name"])), 100),
				Title:        title,
				Kind:         cut(strings.TrimSpace(pyText(it["kind"])), 60),
				Manufacturer: cut(strings.TrimSpace(pyText(it["manufacturer"])), 100),
				Code:         cut(strings.TrimSpace(pyText(it["code"])), 20),
				Image:        image,
			})
		}
		assignMembership(items, p["nameable"])
		gp, _ := p["gift_pending"].(bool)
		out = append(out, &pledge{ID: pid, Name: name, Value: cut(strings.TrimSpace(pyText(p["value"])), 40),
			Created: pyText(p["created"]), GiftPending: gp, Items: items})
	}
	if len(out) == 0 {
		return nil, userErr("None of the pledges had an id and name — has RSI changed the page layout?")
	}
	return out, nil
}

// assignMembership: ships you have named carry an ID of their own, listed beside the pledge's items. RSI repeats that
// list on other pledges too, so an entry is only given to a ship in this pledge showing the same custom name.
func assignMembership(items []*rsiItem, nameable any) {
	list, ok := nameable.([]any)
	if !ok {
		return
	}
	used := map[int]bool{}
	for _, x := range list {
		n, ok := x.(map[string]any)
		if !ok {
			continue
		}
		mid := strings.TrimSpace(pyText(n["membership_id"]))
		custom := normText(pyText(n["custom_name"]))
		if !membershipRe.MatchString(mid) || custom == "" {
			continue
		}
		def := strings.TrimSpace(pyText(n["default_name"]))
		for k, s := range items {
			if !used[k] && strings.ToLower(s.Kind) == "ship" && normText(s.CustomName) == custom && (def == "" || matchLevel(s.Title, def) > 0) {
				used[k] = true
				s.MembershipID = mid
				break
			}
		}
	}
}

// cleanTo: "Apollo Medivac Warbond Edition" -> "Apollo Medivac"; a bare "Warbond Edition" -> "".
func cleanTo(s string) string {
	s = editionRe.ReplaceAllString(strings.TrimSpace(s), "")
	s = bareEditionRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

type upgradeStep struct {
	Date, TargetID, UpgradeID, From, To, ToRaw, Value string
	order                                             int
}

func logLines(raw any) []string {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	if len(list) > 5000 {
		list = list[:5000]
	}
	out := make([]string, len(list))
	for i, l := range list {
		s, isStr := l.(string)
		if !isStr {
			s = pyStr(l)
		}
		out[i] = cut(strings.TrimSpace(s), 1000)
	}
	return out
}

// parseUpgrades reads "Upgrade applied" log lines into steps, oldest first.
func parseUpgrades(raw any) []*upgradeStep {
	steps := []*upgradeStep{}
	for idx, line := range logLines(raw) {
		m := upgradeRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		when, ok := logTime(named(upgradeRe, m, "date"), named(upgradeRe, m, "time"))
		if !ok {
			continue
		}
		steps = append(steps, &upgradeStep{Date: when, TargetID: named(upgradeRe, m, "target_id"), UpgradeID: named(upgradeRe, m, "upgrade_id"),
			From: strings.TrimSpace(named(upgradeRe, m, "from")), To: cleanTo(named(upgradeRe, m, "to")),
			ToRaw: strings.TrimSpace(named(upgradeRe, m, "to")), Value: named(upgradeRe, m, "value"), order: -idx})
	}
	// the log is newest first, so a later line is older
	sort.SliceStable(steps, func(i, j int) bool {
		if steps[i].Date != steps[j].Date {
			return steps[i].Date < steps[j].Date
		}
		return steps[i].order < steps[j].order
	})
	seen := map[string]bool{}
	unique := []*upgradeStep{}
	for _, s := range steps {
		if !seen[s.UpgradeID] {
			seen[s.UpgradeID] = true
			unique = append(unique, s)
		}
	}
	return unique
}

type lifeEvent struct {
	Kind, Date, PledgeID, Name, Amount, To, Target, By, ClaimedBy string
	order                                                         int
}

// parseLifecycle reads the log lines that say what happened to a pledge, oldest first.
func parseLifecycle(raw any) []lifeEvent {
	events := []lifeEvent{}
	g := func(m []string, name string) string { return named(lifecycleRe, m, name) }
	for idx, line := range logLines(raw) {
		m := lifecycleRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		when, ok := logTime(g(m, "date"), g(m, "time"))
		if !ok {
			continue
		}
		var kind, amount string
		switch {
		case g(m, "melt") != "":
			kind, amount = "melted", g(m, "credit")
		case g(m, "gift") != "":
			kind, amount = "gifted", g(m, "gvalue")
		case g(m, "cancelled") != "":
			kind, amount = "cancelled", g(m, "kvalue")
		case g(m, "config") != "":
			kind, amount = "config", g(m, "ccredit")
		case g(m, "created") != "":
			kind, amount = "created", g(m, "cvalue")
		case g(m, "consumed") != "":
			kind, amount = "consumed", g(m, "consvalue")
		default:
			kind, amount = "received", g(m, "rvalue")
		}
		e := lifeEvent{Kind: kind, Date: when, PledgeID: g(m, "id"), Name: strings.TrimSpace(g(m, "name")), Amount: amount, order: -idx}
		if kind == "gifted" {
			e.To = cut(strings.TrimSpace(g(m, "to")), 200)
		}
		if kind == "consumed" {
			e.Target = g(m, "target")
		}
		for _, by := range []string{"mby", "kby", "cfby", "cby", "xby", "rby"} {
			if v := g(m, by); v != "" {
				e.By = strings.TrimSpace(v)
				break
			}
		}
		events = append(events, e)
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Date != events[j].Date {
			return events[i].Date < events[j].Date
		}
		return events[i].order < events[j].order
	})
	return events
}

// logPrices: pledge id -> what it was worth when created (an upgrade's price is the value of its upgrade pledge).
func logPrices(events []lifeEvent) map[string]string {
	prices := map[string]string{}
	for _, e := range events {
		if e.Kind == "created" {
			prices[e.PledgeID] = e.Amount
		}
	}
	for _, e := range events { // an upgrade bought long ago may only be seen being used up
		if _, ok := prices[e.PledgeID]; e.Kind == "consumed" && !ok {
			prices[e.PledgeID] = e.Amount
		}
	}
	return prices
}

// describe gives the status note and who it went to for a melt or gift.
func describe(e lifeEvent) (string, any) {
	if e.Kind == "melted" {
		return "Store credit " + e.Amount, nil
	}
	to := e.To
	if e.ClaimedBy != "" {
		if to != "" {
			to = fmt.Sprintf("%s (%s)", to, e.ClaimedBy)
		} else {
			to = e.ClaimedBy
		}
		return "Value " + e.Amount + " · claimed by the recipient", to
	}
	return "Value " + e.Amount, nilIfEmpty(to)
}

// ---------- the import summary shown in the dialog ----------

type upgradeSummary struct {
	Found       int      `json:"found"`
	Applied     int      `json:"applied"`
	Renamed     int      `json:"renamed"`
	Already     int      `json:"already"`
	Reverted    int      `json:"reverted"`
	Unresolved  int      `json:"unresolved"`
	NotInHangar int      `json:"not_in_hangar"`
	Details     []string `json:"details"`
}

type formerSummary struct {
	Checked           bool             `json:"checked"`
	Events            int              `json:"events"`
	Melted            int              `json:"melted"`
	Gifted            int              `json:"gifted"`
	Missing           int              `json:"missing"`
	Restored          int              `json:"restored"`
	IdentifiedFromLog int              `json:"identified_from_log"`
	CreatedFromLog    int              `json:"created_from_log"`
	GiftedInHangar    []map[string]any `json:"gifted_in_hangar"`
	NotFound          []string         `json:"not_found"`
	NotFoundTotal     int              `json:"not_found_total"`
	Details           []string         `json:"details"`
}

func (f *formerSummary) add(kind string, n int) {
	switch kind {
	case "melted":
		f.Melted += n
	case "gifted":
		f.Gifted += n
	case "missing":
		f.Missing += n
	}
}

type importSummary struct {
	Pledges              int              `json:"pledges"`
	ShipIDsRecorded      int              `json:"ship_ids_recorded"`
	AlreadyImported      int              `json:"already_imported"`
	Matched              int              `json:"matched"`
	Created              int              `json:"created"`
	ItemsCreated         int              `json:"items_created"`
	UnmatchedItems       []string         `json:"unmatched_items"`
	UnmatchedTotal       int              `json:"unmatched_total"`
	Upgrades             upgradeSummary   `json:"upgrades"`
	Former               formerSummary    `json:"former"`
	InsuranceUnclear     []map[string]any `json:"insurance_unclear"`
	InsuranceUpdated     int              `json:"insurance_updated"`
	ShipDifferences      []map[string]any `json:"ship_differences"`
	ShipDifferencesTotal int              `json:"ship_differences_total"`
	ImagesQueued         int              `json:"images_queued"`
	ImagesSkipped        *int             `json:"images_skipped,omitempty"`
	LogNote              any              `json:"log_note,omitempty"`
}

func addDetail(list *[]string, text string) {
	if len(*list) < maxUpgradeDetails {
		*list = append(*list, text)
	}
}

// orderedEvents is a dict of pledge id -> event that remembers insertion order (as Python's dict does).
type orderedEvents struct {
	keys []string
	m    map[string]lifeEvent
}

func (o *orderedEvents) set(k string, e lifeEvent) {
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = e
}

func (o *orderedEvents) del(k string) {
	if _, ok := o.m[k]; !ok {
		return
	}
	delete(o.m, k)
	for i, x := range o.keys {
		if x == k {
			o.keys = append(o.keys[:i:i], o.keys[i+1:]...)
			break
		}
	}
}

// applyLifecycle: packages whose pledge is no longer in the RSI hangar become melted / gifted (never deleted).
func applyLifecycle(tx querier, events []lifeEvent, pledges []*pledge, out *formerSummary) {
	hangar := map[string]string{}
	pending := []string{}
	for _, p := range pledges {
		hangar[p.ID] = p.Name
		if p.GiftPending {
			pending = append(pending, p.ID)
		}
	}
	// The account's own name is the one that creates, melts and uses up pledges.
	counts, order := map[string]int{}, []string{}
	for _, e := range events {
		if (e.Kind == "created" || e.Kind == "melted" || e.Kind == "consumed" || e.Kind == "config") && e.By != "" {
			if counts[e.By] == 0 {
				order = append(order, e.By)
			}
			counts[e.By]++
		}
	}
	owner := ""
	for _, n := range order {
		if counts[n] > counts[owner] {
			owner = n
		}
	}
	latest := &orderedEvents{m: map[string]lifeEvent{}}
	for _, e := range events {
		switch {
		case e.Kind == "melted" || e.Kind == "gifted":
			latest.set(e.PledgeID, e)
		case e.Kind == "received" && owner != "" && e.By == owner: // you claimed it (back)
			latest.del(e.PledgeID)
		case e.Kind == "cancelled" && latest.m[e.PledgeID].Kind == "gifted": // you took the gift back
			latest.del(e.PledgeID)
		}
	}
	// The recipient's claim is logged at the same minute as the gift, in no particular order, so it is matched afterwards.
	for _, e := range events {
		g, ok := latest.m[e.PledgeID]
		if e.Kind == "received" && !(owner != "" && e.By == owner) && ok && g.Kind == "gifted" && e.Date >= g.Date {
			g.ClaimedBy = e.By
			latest.set(e.PledgeID, g)
		}
	}
	created := map[string]lifeEvent{}
	for _, e := range events {
		if e.Kind == "created" {
			created[e.PledgeID] = e
		}
	}

	// A gift nobody has claimed yet is still in your hangar (RSI shows a Cancel Gift button), so it stays owned.
	listed := map[string]bool{}
	isPending := map[string]bool{}
	for _, pid := range pending {
		isPending[pid] = true
	}
	for _, pid := range latest.keys {
		e := latest.m[pid]
		if _, inHangar := hangar[pid]; e.Kind == "gifted" && inHangar {
			listed[pid] = true
			out.GiftedInHangar = append(out.GiftedInHangar, map[string]any{"package": hangar[pid], "date": e.Date[:10], "pending": isPending[pid]})
			_, to := describe(e)
			mustExec(tx, "UPDATE packages SET gifted_to = ? WHERE rsi_pledge_id = ? AND status = 'owned' AND gift_pending = 1", to, pid)
		}
	}
	for _, pid := range pending {
		if !listed[pid] { // RSI says it is pending, but the log has no gift entry for it
			out.GiftedInHangar = append(out.GiftedInHangar, map[string]any{"package": hangar[pid], "date": nil, "pending": true})
		}
	}

	mark := func(pkgID any, name string, e *lifeEvent) {
		status, date, note, to := "missing", any(nil), any(nil), any(nil)
		if e != nil {
			status, date = e.Kind, e.Date[:10]
			n, t := describe(*e)
			note, to = n, t
		}
		mustExec(tx, "UPDATE packages SET status = ?, status_date = ?, status_note = ?, gifted_to = ?, updated_at = datetime('now') WHERE id = ?",
			status, date, note, to, pkgID)
		out.add(status, 1)
		label := map[string]string{"melted": "Melted", "gifted": "Gifted away", "missing": "No longer in your hangar"}[status]
		when := ""
		if e != nil {
			when = " " + e.Date[:10]
		}
		addDetail(&out.Details, label+when+": "+name) // never the recipient: this text is shown on screen
	}

	for _, r := range mustRows(tx, "SELECT id, name, rsi_pledge_id, status FROM packages WHERE rsi_pledge_id IS NOT NULL") {
		pid := str(r["rsi_pledge_id"])
		if _, inHangar := hangar[pid]; inHangar {
			if str(r["status"]) != "owned" { // it is in the hangar again, whatever the list said
				mustExec(tx, "UPDATE packages SET status = 'owned', status_date = NULL, status_note = NULL, gifted_to = NULL WHERE id = ?", r["id"])
				out.Restored++
			}
			continue
		}
		e, known := latest.m[pid]
		status := str(r["status"])
		if status == "owned" || (status == "missing" && known) { // 'missing' becomes melted/gifted once the log says so
			if status == "missing" {
				out.Missing--
			}
			if known {
				mark(r["id"], str(r["name"]), &e)
			} else {
				mark(r["id"], str(r["name"]), nil)
			}
		}
	}

	// Packages in your list that RSI never matched (no pledge id): the log may know them as melted or gifted.
	knownIDs := map[string]bool{}
	for _, r := range mustRows(tx, "SELECT rsi_pledge_id FROM packages WHERE rsi_pledge_id IS NOT NULL") {
		knownIDs[str(r["rsi_pledge_id"])] = true
	}
	byName := map[string][]lifeEvent{}
	for _, pid := range latest.keys {
		if _, inHangar := hangar[pid]; !knownIDs[pid] && !inHangar {
			e := latest.m[pid]
			byName[normText(e.Name)] = append(byName[normText(e.Name)], e)
		}
	}
	for _, r := range mustRows(tx, "SELECT id, name FROM packages WHERE rsi_pledge_id IS NULL AND status = 'owned' ORDER BY sort_order, id") {
		key := normText(str(r["name"]))
		if c := byName[key]; len(c) > 0 {
			e := c[0]
			byName[key] = c[1:]
			mustExec(tx, "UPDATE packages SET rsi_pledge_id = ? WHERE id = ?", e.PledgeID, r["id"])
			mark(r["id"], str(r["name"]), &e)
			out.IdentifiedFromLog++
		} else {
			out.NotFoundTotal++
			if len(out.NotFound) < maxUnmatchedReported {
				out.NotFound = append(out.NotFound, str(r["name"]))
			}
		}
	}

	// A full-history database (started with "Start fresh") also remembers pledges that have left the hangar. The log
	// only knows their name, date and amounts, so they are packages without items.
	if v, _ := getMeta(tx, "full_history"); v == "1" {
		knownIDs = map[string]bool{}
		for _, r := range mustRows(tx, "SELECT rsi_pledge_id FROM packages WHERE rsi_pledge_id IS NOT NULL") {
			knownIDs[str(r["rsi_pledge_id"])] = true
		}
		for _, pid := range latest.keys {
			if _, inHangar := hangar[pid]; knownIDs[pid] || inHangar {
				continue
			}
			e := latest.m[pid]
			c, hasCreated := created[pid]
			order := queryInt(tx, "SELECT COALESCE(MAX(sort_order), 0) + 1 FROM packages")
			note, to := describe(e)
			var date, purchase any
			if hasCreated {
				date, purchase = c.Date[:10], c.Amount
			}
			mustExec(tx, "INSERT INTO packages (name, rsi_pledge_id, pledge_date, value, purchase_value, status, status_date, status_note,"+
				" gifted_to, sort_order) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
				e.Name, pid, date, e.Amount, purchase, e.Kind, e.Date[:10], note, to, order)
			out.add(e.Kind, 1)
			out.CreatedFromLog++
		}
	}
}

// buildChains groups one pledge's steps (oldest first) into per-ship chains: HoverQuad -> X1 Force -> Apollo.
func buildChains(steps []*upgradeStep) [][]*upgradeStep {
	chains := [][]*upgradeStep{}
	for _, s := range steps {
		best, bestLevel := -1, 0
		for i, ch := range chains {
			if last := ch[len(ch)-1]; last.To != "" {
				if level := matchLevel(last.To, s.From); level > bestLevel {
					best, bestLevel = i, level
				}
			}
		}
		if best < 0 {
			// RSI sometimes names an upgrade without the target ship ("... to Warbond Edition"); the next upgrade
			// starting from a ship nobody else produced continues that chain.
			for i, ch := range chains {
				if ch[len(ch)-1].To == "" {
					best = i
					break
				}
			}
		}
		if best < 0 {
			chains = append(chains, []*upgradeStep{s})
		} else {
			chains[best] = append(chains[best], s)
		}
	}
	return chains
}

// insuranceLabel: "Lifetime Insurance" -> "Lifetime", "120 Month Insurance" -> "120 months", "10 Year" -> "10 years".
func insuranceLabel(title string) string {
	t := strings.TrimSpace(title)
	if lifetimeRe.MatchString(t) {
		return "Lifetime"
	}
	if m := termRe.FindStringSubmatch(t); m != nil {
		n, _ := strconv.Atoi(m[1])
		plural := "s"
		if n == 1 {
			plural = ""
		}
		return fmt.Sprintf("%d %s%s", n, strings.ToLower(m[2]), plural)
	}
	return strings.TrimSpace(insuranceRe.ReplaceAllString(t, ""))
}

func insuranceRank(label string) float64 {
	if label == "Lifetime" {
		return math.Inf(1)
	}
	m := rankRe.FindStringSubmatch(label)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	if m[2] == "year" {
		return float64(n * 12)
	}
	return float64(n)
}

func pledgeLabels(p *pledge) []string {
	out := []string{}
	for _, i := range p.Items {
		if isInsurance(i) {
			if l := insuranceLabel(i.Title); l != "" {
				out = append(out, l)
			}
		}
	}
	return out
}

// pledgeInsurance gives the ship titles of a pledge with the insurance they get, and whether several terms were listed.
// When a ship could have more than one of the listed terms, the longest applies.
func pledgeInsurance(p *pledge) (ships []string, best string, unclear bool) {
	labels := pledgeLabels(p)
	seen := map[string]bool{}
	for _, i := range p.Items {
		if strings.ToLower(i.Kind) == "ship" && !seen[i.Title] {
			seen[i.Title] = true
			ships = append(ships, i.Title)
		}
	}
	if len(labels) == 0 || len(ships) == 0 {
		return nil, "", false
	}
	best = labels[0]
	distinct := map[string]bool{}
	for _, l := range labels {
		distinct[l] = true
		if insuranceRank(l) > insuranceRank(best) {
			best = l
		}
	}
	return ships, best, len(distinct) > 1
}

// applyInsurance keeps the Insurance of ships already in the database in step with RSI.
func applyInsurance(tx querier, pledges []*pledge, packageByRSI map[string]int64, summary *importSummary) {
	changed := 0
	summary.InsuranceUnclear = []map[string]any{}
	for _, p := range pledges {
		ships, best, unclear := pledgeInsurance(p)
		pkgID, ok := packageByRSI[p.ID]
		if len(ships) == 0 || !ok {
			continue
		}
		if unclear {
			summary.InsuranceUnclear = append(summary.InsuranceUnclear, map[string]any{"package": p.Name, "ships": len(ships),
				"entries": pledgeLabels(p), "used": best})
		}
		for _, r := range mustRows(tx, "SELECT id, name, insurance FROM items WHERE package_id = ?", pkgID) {
			label := ""
			for _, title := range ships {
				if matchLevel(str(r["name"]), title) == 2 {
					label = best
					break
				}
			}
			current := str(r["insurance"])
			if label == "" || current == label {
				continue
			}
			if unclear && current != "" && !strings.Contains(current, "/") {
				continue // in a package with several terms, anything but the old automatic mix was set by you: keep it
			}
			mustExec(tx, "UPDATE items SET insurance = ?, updated_at = datetime('now') WHERE id = ?", label, r["id"])
			changed++
		}
	}
	summary.InsuranceUpdated = changed
}

// resolveManufacturer prefers the spelling already used in the database ("RSI", "Argo") over RSI's long name.
func resolveManufacturer(full, code string, known []string) string {
	if full == "" && code == "" {
		return ""
	}
	for _, k := range known {
		kn := normText(k)
		if kn != "" && (kn == normText(code) || kn == normText(full) || strings.HasPrefix(normText(full), kn+" ")) {
			return k
		}
	}
	if full != "" {
		return full
	}
	return code
}

// unclaimedPackages: normalised name -> ids of packages without a pledge id, remembering the order names were seen.
type unclaimedPackages struct {
	keys []string
	m    map[string][]int64
}

func (u *unclaimedPackages) add(name string, id int64) {
	if _, ok := u.m[name]; !ok {
		u.keys = append(u.keys, name)
	}
	u.m[name] = append(u.m[name], id)
}

// preassignDuplicates: when several pledges share a name (three "Standalone Ship - HoverQuad"), pair them with the
// existing packages by what is inside: a package holding "HULL A" belongs to the pledge that became a Hull A.
func preassignDuplicates(tx querier, pledges []*pledge, unclaimed *unclaimedPackages, stepsByPledge map[string][]*upgradeStep) map[string]int64 {
	type member struct {
		idx int
		p   *pledge
	}
	groups, groupOrder := map[string][]member{}, []string{}
	for idx, p := range pledges {
		key := normText(p.Name)
		if _, ok := groups[key]; !ok {
			groupOrder = append(groupOrder, key)
		}
		groups[key] = append(groups[key], member{idx, p})
	}
	result := map[string]int64{}
	for _, name := range groupOrder {
		group := groups[name]
		cands := append([]int64{}, unclaimed.m[name]...)
		if len(group) < 2 || len(cands) < 2 {
			continue
		}
		names := map[string][]string{}
		for _, g := range group {
			n := []string{}
			for _, i := range g.p.Items {
				if !isInsurance(i) {
					n = append(n, i.Title)
				}
			}
			for _, s := range stepsByPledge[g.p.ID] {
				n = append(n, s.From, s.To)
			}
			names[g.p.ID] = n
		}
		type key struct {
			pid string
			pk  int64
		}
		score := map[key]float64{}
		for _, pk := range cands {
			for _, r := range mustRows(tx, "SELECT name FROM items WHERE package_id = ?", pk) {
				owners := []string{}
				for _, g := range group {
					for _, x := range names[g.p.ID] {
						if matchLevel(str(r["name"]), x) == 2 {
							owners = append(owners, g.p.ID)
							break
						}
					}
				}
				for _, pid := range owners {
					score[key{pid, pk}] += 1 / float64(len(owners)) // a name every pledge shares ("HoverQuad") says little
				}
			}
		}
		order := map[string]int{}
		for _, g := range group {
			order[g.p.ID] = g.idx
		}
		type pair struct {
			score float64
			pid   string
			pk    int64
			pkIdx int
		}
		pairs := []pair{}
		for _, g := range group {
			for i, pk := range cands {
				pairs = append(pairs, pair{score[key{g.p.ID, pk}], g.p.ID, pk, i})
			}
		}
		sort.SliceStable(pairs, func(i, j int) bool { // ties: oldest pledge, first package
			a, b := pairs[i], pairs[j]
			if a.score != b.score {
				return a.score > b.score
			}
			if order[a.pid] != order[b.pid] {
				return order[a.pid] > order[b.pid]
			}
			return a.pkIdx < b.pkIdx
		})
		usedP, usedK := map[string]bool{}, map[int64]bool{}
		for _, pr := range pairs {
			if usedP[pr.pid] || usedK[pr.pk] {
				continue
			}
			result[pr.pid] = pr.pk
			usedP[pr.pid] = true
			usedK[pr.pk] = true
		}
		rest := []int64{}
		for _, pk := range unclaimed.m[name] {
			if !usedK[pk] {
				rest = append(rest, pk)
			}
		}
		unclaimed.m[name] = rest
	}
	return result
}

// historyStep is one upgrade in items.upgrade_history, in the field order the Python Hangar wrote.
type historyStep struct {
	From      string         `json:"from"`
	To        string         `json:"to"`
	Date      string         `json:"date"`
	UpgradeID string         `json:"upgrade_id"`
	Value     string         `json:"value"`
	Price     any            `json:"price"`
	Before    map[string]any `json:"before,omitempty"`
}

func toJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
	return strings.TrimRight(buf.String(), "\n")
}

// addMissingPrices: upgrades recorded by an earlier import get their price once the log supplies it.
func addMissingPrices(tx querier, prices map[string]string) {
	for _, r := range mustRows(tx, "SELECT id, upgrade_history FROM items WHERE upgrade_history IS NOT NULL") {
		var history []map[string]any
		if json.Unmarshal([]byte(str(r["upgrade_history"])), &history) != nil {
			continue
		}
		changed := false
		for _, s := range history {
			if pyText(s["price"]) == "" {
				if p := prices[pyText(s["upgrade_id"])]; p != "" {
					s["price"] = p
					changed = true
				}
			}
		}
		if changed {
			mustExec(tx, "UPDATE items SET upgrade_history = ? WHERE id = ?", toJSON(history), r["id"])
		}
	}
}

func applyUpgrades(tx querier, steps []*upgradeStep, pledgesByID map[string]*pledge, packageByRSI map[string]int64,
	knownMakers []string, summary *importSummary, prices map[string]string) {
	out := &summary.Upgrades
	appliedIDs := map[string]bool{}
	for _, r := range mustRows(tx, "SELECT upgrade_history FROM items WHERE upgrade_history IS NOT NULL") {
		var history []map[string]any
		if json.Unmarshal([]byte(str(r["upgrade_history"])), &history) == nil {
			for _, s := range history {
				appliedIDs[pyText(s["upgrade_id"])] = true
			}
		}
	}
	typeByName := map[string]string{}
	for _, r := range mustRows(tx, "SELECT name, type FROM items WHERE type IS NOT NULL") {
		if _, ok := typeByName[core(str(r["name"]))]; !ok {
			typeByName[core(str(r["name"]))] = str(r["type"])
		}
	}
	byPledge, pledgeOrder := map[string][]*upgradeStep{}, []string{}
	for _, s := range steps {
		if _, ok := byPledge[s.TargetID]; !ok {
			pledgeOrder = append(pledgeOrder, s.TargetID)
		}
		byPledge[s.TargetID] = append(byPledge[s.TargetID], s)
	}

	for _, pid := range pledgeOrder {
		pledgeSteps := byPledge[pid]
		pkgID, ok := packageByRSI[pid]
		if !ok {
			out.NotInHangar += len(pledgeSteps)
			continue
		}
		p := pledgesByID[pid]
		var current []*rsiItem
		if p != nil {
			current = []*rsiItem{}
			for _, i := range p.Items {
				if !isInsurance(i) {
					current = append(current, i)
				}
			}
		}
		rows := mustRows(tx, "SELECT * FROM items WHERE package_id = ? ORDER BY sort_order, id", pkgID)
		taken := map[int64]bool{}
		for _, chain := range buildChains(pledgeSteps) {
			all := true
			for _, s := range chain {
				if !appliedIDs[s.UpgradeID] {
					all = false
				}
			}
			if all {
				out.Already++
				continue
			}
			for k, s := range chain { // fill a missing target from the next step's starting ship
				if s.To == "" && k+1 < len(chain) {
					s.To = chain[k+1].From
				}
			}

			var curItem *rsiItem
			kept := chain
			if current != nil {
				// What RSI shows today is the truth: keep the chain only as far as the pledge's contents confirm it.
				kept = nil
				for k := len(chain) - 1; k >= 0; k-- {
					var hit *rsiItem
					bestLevel := -1
					for _, i := range current {
						if l := matchLevel(chain[k].To, i.Title); l > bestLevel {
							hit, bestLevel = i, l
						}
					}
					if chain[k].To != "" && hit != nil && matchLevel(chain[k].To, hit.Title) > 0 {
						kept, curItem = chain[:k+1], hit
						break
					}
				}
				if len(kept) == 0 {
					started := false
					for _, i := range current {
						if matchLevel(chain[0].From, i.Title) > 0 {
							started = true
						}
					}
					last := chain[len(chain)-1]
					to := last.To
					if to == "" {
						to = last.ToRaw
					}
					if started {
						out.Reverted++
						addDetail(&out.Details, fmt.Sprintf("No longer applied: %s → %s (%s)", chain[0].From, to, p.Name))
					} else {
						out.Unresolved++
						addDetail(&out.Details, fmt.Sprintf("Could not place: %s → %s (%s)", chain[0].From, to, p.Name))
					}
					continue
				}
			}
			final := kept[len(kept)-1].To
			if curItem != nil {
				final = curItem.Title
			}

			wanted := []string{final}
			for k := len(kept) - 2; k >= 0; k-- {
				wanted = append(wanted, kept[k].To)
			}
			wanted = append(wanted, kept[0].From)
			var chosen row
			for _, name := range wanted {
				bestLevel, bestN := 0, -1
				for n, r := range rows {
					id, _ := r["id"].(int64)
					if taken[id] || nonVehicleTypes[strings.ToLower(str(r["type"]))] {
						continue
					}
					if l := matchLevel(name, str(r["name"])); l > bestLevel {
						bestLevel, bestN = l, n
					}
				}
				if bestN >= 0 {
					chosen = rows[bestN]
					break
				}
			}
			if chosen == nil {
				out.Unresolved++
				where := pid
				if p != nil {
					where = p.Name
				}
				addDetail(&out.Details, fmt.Sprintf("Could not find the ship in the package: %s → %s (%s)", kept[0].From, final, where))
				continue
			}
			chosenID, _ := chosen["id"].(int64)
			taken[chosenID] = true

			history := make([]*historyStep, len(kept))
			for k, s := range kept {
				to := s.To
				if to == "" {
					to = final
				}
				var price any
				if pr, ok := prices[s.UpgradeID]; ok {
					price = pr
				}
				history[k] = &historyStep{From: s.From, To: to, Date: s.Date, UpgradeID: s.UpgradeID, Value: s.Value, Price: price}
			}
			history[len(history)-1].To = final
			sets, args := []string{}, []any{}
			set := func(k string, v any) { sets = append(sets, k+" = ?"); args = append(args, v) }
			chosenName := str(chosen["name"])
			if matchLevel(chosenName, final) < 2 {
				before := map[string]any{}
				for _, k := range []string{"name", "type", "manufacturer", "purpose", "crew", "storage_scu", "flight_ready"} {
					if pyText(chosen[k]) != "" {
						before[k] = chosen[k]
					}
				}
				history[0].Before = before
				typ := typeByName[core(final)]
				if typ == "" && curItem != nil {
					typ = curItem.Kind
				}
				var typeValue any = nilIfEmpty(typ)
				if typ == "" {
					typeValue = chosen["type"]
				}
				set("name", final)
				set("purpose", nil)
				set("crew", nil)
				set("storage_scu", nil)
				set("flight_ready", nil)
				set("type", typeValue)
				if curItem != nil {
					if m := resolveManufacturer(curItem.Manufacturer, curItem.Code, knownMakers); m != "" {
						set("manufacturer", m)
					} else {
						set("manufacturer", chosen["manufacturer"])
					}
				}
				out.Renamed++
			} else if stripped := strings.TrimSpace(markerRe.ReplaceAllString(chosenName, "")); stripped != chosenName {
				set("name", stripped)
			}
			set("upgrade_history", toJSON(history))
			args = append(args, chosenID)
			mustExec(tx, "UPDATE items SET "+strings.Join(sets, ", ")+", updated_at = datetime('now') WHERE id = ?", args...)
			out.Applied++
			parts := []string{kept[0].From}
			for _, h := range history {
				parts = append(parts, h.To)
			}
			text := strings.Join(parts, " → ")
			if history[0].Before != nil {
				text += fmt.Sprintf(" (was “%s” in your list)", chosenName)
			}
			addDetail(&out.Details, text)
			for _, s := range kept {
				appliedIDs[s.UpgradeID] = true
			}
		}
	}
}

type imageTask struct {
	itemID int64
	url    string
}

// importPledges merges an RSI read into the database. All or nothing: if anything goes wrong part-way, nothing of
// this import is kept. complete=true means every page of the hangar was read, so a package whose pledge is absent
// really is gone (melted, gifted...); without it nothing is marked as former. The caller holds dbMu.
func importPledges(rawPledges, rawUpgrades, rawEvents any, complete bool) (summary *importSummary, tasks []imageTask, err error) {
	pledges, err := cleanPledges(rawPledges)
	if err != nil {
		return nil, nil, err
	}
	steps := parseUpgrades(rawUpgrades)
	events := parseLifecycle(rawEvents)
	if _, err := snapshot("before-rsi-import", -1); err != nil {
		return nil, nil, err
	}
	tx, err := store.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			if e, ok := r.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("%v", r)
			}
			summary, tasks = nil, nil
		}
	}()
	summary, tasks = runImport(tx, pledges, steps, events, complete)
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	summary.ImagesQueued = len(tasks)
	return summary, tasks, nil
}

func runImport(tx *sql.Tx, pledges []*pledge, steps []*upgradeStep, events []lifeEvent, complete bool) (*importSummary, []imageTask) {
	stepsByPledge := map[string][]*upgradeStep{}
	for _, s := range steps {
		stepsByPledge[s.TargetID] = append(stepsByPledge[s.TargetID], s)
	}
	configEvents := map[string][]lifeEvent{}
	for _, e := range events {
		if e.Kind == "config" {
			configEvents[e.PledgeID] = append(configEvents[e.PledgeID], e)
		}
	}
	summary := &importSummary{Pledges: len(pledges), UnmatchedItems: []string{},
		Upgrades: upgradeSummary{Found: len(steps), Details: []string{}},
		Former: formerSummary{Checked: complete, Events: len(events), GiftedInHangar: []map[string]any{}, NotFound: []string{},
			Details: []string{}}}
	prices := logPrices(events)
	createdValue := map[string]string{}
	for _, e := range events {
		if e.Kind == "created" {
			createdValue[e.PledgeID] = e.Amount
		}
	}
	createdValueOf := func(id string) any {
		if v, ok := createdValue[id]; ok {
			return v
		}
		return nil
	}
	tasks := []imageTask{}
	if _, has := getMeta(tx, "full_history"); complete && !has && queryInt(tx, "SELECT COUNT(*) FROM packages") == 0 {
		mustNil(setMeta(tx, "full_history", "1")) // a new user's first import also remembers melted and gifted pledges
	}

	importedIDs := map[string]bool{}
	packageByRSI := map[string]int64{}
	for _, r := range mustRows(tx, "SELECT id, rsi_pledge_id FROM packages WHERE rsi_pledge_id IS NOT NULL") {
		importedIDs[str(r["rsi_pledge_id"])] = true
		packageByRSI[str(r["rsi_pledge_id"])] = r["id"].(int64)
	}
	unclaimed := &unclaimedPackages{m: map[string][]int64{}}
	for _, r := range mustRows(tx, "SELECT id, name FROM packages WHERE rsi_pledge_id IS NULL AND status = 'owned' ORDER BY sort_order, id") {
		unclaimed.add(normText(str(r["name"])), r["id"].(int64))
	}
	knownMakers := []string{}
	for _, r := range mustRows(tx, "SELECT DISTINCT manufacturer FROM items WHERE manufacturer IS NOT NULL") {
		knownMakers = append(knownMakers, str(r["manufacturer"]))
	}
	pledgesByID := map[string]*pledge{}
	for _, p := range pledges {
		pledgesByID[p.ID] = p
	}

	// Exact name first; otherwise a name that differs only by a prefix such as "Standalone Ships -", provided the two
	// packages share at least one item (so unrelated packages are never merged).
	findUnclaimed := func(p *pledge) (int64, bool) {
		rsiName := normText(p.Name)
		if ids := unclaimed.m[rsiName]; len(ids) > 0 {
			unclaimed.m[rsiName] = ids[1:]
			return ids[0], true
		}
		titles := map[string]bool{}
		for _, i := range p.Items {
			titles[normText(i.Title)] = true
		}
		for _, name := range unclaimed.keys {
			ids := unclaimed.m[name]
			if len(ids) == 0 || len([]rune(name)) < 8 || !(strings.HasSuffix(rsiName, name) || strings.HasSuffix(name, rsiName)) {
				continue
			}
			for _, r := range mustRows(tx, "SELECT name FROM items WHERE package_id = ?", ids[0]) {
				if titles[normText(str(r["name"]))] {
					unclaimed.m[name] = ids[1:]
					return ids[0], true
				}
			}
		}
		return 0, false
	}

	type check struct {
		pkgID  int64
		p      *pledge
		report bool
	}
	checks := []check{} // pictures are matched after upgrades rename things

	// Phase A: work out which package each pledge is. RSI lists newest first; create oldest first so the newest ends
	// up at the top of the list.
	todo := []*pledge{}
	for _, p := range pledges {
		if !importedIDs[p.ID] {
			todo = append(todo, p)
		}
	}
	assigned := preassignDuplicates(tx, todo, unclaimed, stepsByPledge)
	for k := len(pledges) - 1; k >= 0; k-- {
		p := pledges[k]
		if importedIDs[p.ID] {
			summary.AlreadyImported++
			checks = append(checks, check{packageByRSI[p.ID], p, false})
			continue
		}
		pkgID, found := assigned[p.ID]
		if !found {
			pkgID, found = findUnclaimed(p)
		}
		if found {
			mustExec(tx, "UPDATE packages SET rsi_pledge_id = ?, pledge_date = COALESCE(pledge_date, ?), "+
				"value = COALESCE(value, ?), updated_at = datetime('now') WHERE id = ?",
				p.ID, parseCreated(p.Created), nilIfEmpty(p.Value), pkgID)
			packageByRSI[p.ID] = pkgID
			summary.Matched++
			checks = append(checks, check{pkgID, p, true})
			continue
		}
		order := queryInt(tx, "SELECT COALESCE(MIN(sort_order), 1) - 1 FROM packages")
		res := mustExec(tx, "INSERT INTO packages (name, rsi_pledge_id, pledge_date, value, purchase_value, sort_order) VALUES (?, ?, ?, ?, ?, ?)",
			p.Name, p.ID, parseCreated(p.Created), nilIfEmpty(p.Value), createdValueOf(p.ID), order)
		pkgID, _ = res.LastInsertId()
		packageByRSI[p.ID] = pkgID
		summary.Created++
		ships, best, _ := pledgeInsurance(p)
		n := 0
		for _, it := range p.Items {
			if isInsurance(it) {
				continue
			}
			var insurance any
			if strings.ToLower(it.Kind) == "ship" && contains(ships, it.Title) {
				insurance = best
			}
			res := mustExec(tx, "INSERT INTO items (package_id, type, name, manufacturer, insurance, sort_order, custom_name, membership_id) "+
				"VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
				pkgID, nilIfEmpty(it.Kind), it.Title, nilIfEmpty(resolveManufacturer(it.Manufacturer, it.Code, knownMakers)),
				insurance, n, nilIfEmpty(it.CustomName), nilIfEmpty(it.MembershipID))
			n++
			itemID, _ := res.LastInsertId()
			summary.ItemsCreated++
			if it.Image != "" {
				tasks = append(tasks, imageTask{itemID, it.Image})
			}
		}
	}

	// What each pledge is worth now (RSI's figure, which includes upgrades) and what it was worth when bought (the log).
	for _, p := range pledges {
		gp := 0
		if p.GiftPending {
			gp = 1
		}
		mustExec(tx, "UPDATE packages SET value = COALESCE(?, value), purchase_value = COALESCE(purchase_value, ?), gift_pending = ?, "+
			"gifted_to = CASE WHEN ? = 1 THEN gifted_to ELSE CASE WHEN status = 'owned' THEN NULL ELSE gifted_to END END "+
			"WHERE rsi_pledge_id = ?", nilIfEmpty(p.Value), createdValueOf(p.ID), gp, gp, p.ID)
	}

	// Phase B: ships that were upgraded are renamed to what you own now, keeping the history.
	if len(steps) > 0 {
		applyUpgrades(tx, steps, pledgesByID, packageByRSI, knownMakers, summary, prices)
		addMissingPrices(tx, prices)
	}

	// Phase B2: packages whose pledge has left the hangar (melted, gifted away...).
	if complete {
		applyLifecycle(tx, events, pledges, &summary.Former)
	}

	applyInsurance(tx, pledges, packageByRSI, summary)

	// Phase C: pictures, and a report of RSI items the existing package doesn't have.
	for _, c := range checks {
		existing := map[string][]row{}
		for _, r := range mustRows(tx, "SELECT id, name, image, custom_name, membership_id FROM items WHERE package_id = ? ORDER BY sort_order, id", c.pkgID) {
			k := normText(str(r["name"]))
			existing[k] = append(existing[k], r)
		}
		for _, it := range c.p.Items {
			if isInsurance(it) {
				continue
			}
			key := normText(it.Title)
			candidates := existing[key]
			if len(candidates) == 0 {
				if c.report {
					summary.UnmatchedTotal++
					if len(summary.UnmatchedItems) < maxUnmatchedReported {
						summary.UnmatchedItems = append(summary.UnmatchedItems, c.p.Name+": "+it.Title)
					}
				}
				continue
			}
			r := candidates[0]
			candidates = candidates[1:]
			if it.MembershipID != "" { // a ship you can rename: remember its own ID, preferring the row that already has it
				sameAt := -1
				for i, x := range candidates {
					if str(x["membership_id"]) == it.MembershipID {
						sameAt = i
						break
					}
				}
				if str(r["membership_id"]) != it.MembershipID && sameAt >= 0 {
					same := candidates[sameAt]
					rest := append([]row{r}, candidates[:sameAt]...)
					candidates = append(rest, candidates[sameAt+1:]...)
					r = same
				}
				if str(r["membership_id"]) != it.MembershipID || str(r["custom_name"]) != it.CustomName {
					mustExec(tx, "UPDATE items SET membership_id = ?, custom_name = ?, updated_at = datetime('now') WHERE id = ?",
						it.MembershipID, nilIfEmpty(it.CustomName), r["id"])
					summary.ShipIDsRecorded++
				}
			}
			existing[key] = candidates
			if it.Image != "" && str(r["image"]) == "" {
				tasks = append(tasks, imageTask{r["id"].(int64), it.Image})
			}
		}
	}

	// Phase D: RSI is the authority on which ships you own, so list every pledge where the ships in your list differ
	// from RSI's current contents (a wrong ship on a package, a typo, a missing ship).
	differences := []map[string]any{}
	for _, p := range pledges {
		var pkgID any
		if id, ok := packageByRSI[p.ID]; ok {
			pkgID = id
		}
		onlyRSI := []string{}
		for _, i := range p.Items {
			if i.Kind == "Ship" {
				onlyRSI = append(onlyRSI, i.Title)
			}
		}
		onlyMine := []string{}
		for _, r := range mustRows(tx, "SELECT name FROM items WHERE package_id = ? AND lower(coalesce(type, '')) IN ('ship', 'ground', 'hover bike')", pkgID) {
			hit := -1
			for i, x := range onlyRSI {
				if matchLevel(str(r["name"]), x) == 2 {
					hit = i
					break
				}
			}
			if hit >= 0 {
				onlyRSI = append(onlyRSI[:hit:hit], onlyRSI[hit+1:]...)
			} else {
				onlyMine = append(onlyMine, str(r["name"]))
			}
		}
		if len(onlyRSI) > 0 || len(onlyMine) > 0 {
			notes := []string{}
			for _, e := range configEvents[p.ID] {
				notes = append(notes, fmt.Sprintf("the log shows a ship was reclaimed for %s on %s", e.Amount, e.Date[:10]))
			}
			differences = append(differences, map[string]any{"pledge": p.Name, "rsi": onlyRSI, "yours": onlyMine,
				"note": nilIfEmpty(strings.Join(notes, "; "))})
		}
	}
	summary.ShipDifferencesTotal = len(differences)
	if len(differences) > maxUnmatchedReported {
		differences = differences[:maxUnmatchedReported]
	}
	summary.ShipDifferences = differences
	return summary, tasks
}

func mustNil(err error) {
	if err != nil {
		panic(err)
	}
}
