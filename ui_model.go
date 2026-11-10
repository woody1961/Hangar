package main

// What the desktop window shows, worked out from the database: packages and items, search, column filters, sorting,
// the "do I already own this?" answer and package totals. The rules are the original Hangar screens'.

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

type Package struct {
	ID                                                  int64
	Name, RSIPledgeID, PledgeDate, Value, PurchaseValue string
	Notes, Status, StatusDate, StatusNote, GiftedTo     string
	SortOrder                                           int64
	GiftPending                                         bool
}

type HistoryStep struct {
	From, To, Date, UpgradeID, Value, Price string
	BeforeName, BeforePurpose               string
}

type Item struct {
	ID, PackageID                                      int64
	Type, Name, Manufacturer, Purpose, Insurance, Crew string
	StorageSCU, FlightReady, Notes, Image              string
	CustomName, MembershipID                           string
	SortOrder                                          int64
	History                                            []HistoryStep
	spaced, compact                                    string
}

// text reads any stored value as text (a number stored by an older version reads as its digits).
func txt(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		if x == math.Trunc(x) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

func num(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case float64:
		return int64(x)
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	}
	return 0
}

func parseHistory(raw string) []HistoryStep {
	if raw == "" {
		return nil
	}
	var list []map[string]any
	if json.Unmarshal([]byte(raw), &list) != nil {
		return nil
	}
	out := []HistoryStep{}
	for _, h := range list {
		s := HistoryStep{From: txt(h["from"]), To: txt(h["to"]), Date: txt(h["date"]), UpgradeID: txt(h["upgrade_id"]),
			Value: txt(h["value"]), Price: txt(h["price"])}
		if b, ok := h["before"].(map[string]any); ok {
			s.BeforeName, s.BeforePurpose = txt(b["name"]), txt(b["purpose"])
		}
		out = append(out, s)
	}
	return out
}

// Hangar is everything the window knows about the data, rebuilt after every change.
type Hangar struct {
	Packages []*Package
	Items    []*Item
	byID     map[int64]*Package
}

func loadHangar() (*Hangar, error) {
	dbMu.Lock()
	data, err := allData(store)
	dbMu.Unlock()
	if err != nil {
		return nil, err
	}
	h := &Hangar{byID: map[int64]*Package{}}
	for _, r := range data["packages"].([]row) {
		p := &Package{ID: num(r["id"]), Name: txt(r["name"]), RSIPledgeID: txt(r["rsi_pledge_id"]), PledgeDate: txt(r["pledge_date"]),
			Value: txt(r["value"]), PurchaseValue: txt(r["purchase_value"]), Notes: txt(r["notes"]), Status: txt(r["status"]),
			StatusDate: txt(r["status_date"]), StatusNote: txt(r["status_note"]), GiftedTo: txt(r["gifted_to"]),
			SortOrder: num(r["sort_order"]), GiftPending: num(r["gift_pending"]) != 0}
		if p.Status == "" {
			p.Status = "owned"
		}
		h.Packages = append(h.Packages, p)
		h.byID[p.ID] = p
	}
	for _, r := range data["items"].([]row) {
		it := &Item{ID: num(r["id"]), PackageID: num(r["package_id"]), Type: txt(r["type"]), Name: txt(r["name"]),
			Manufacturer: txt(r["manufacturer"]), Purpose: txt(r["purpose"]), Insurance: txt(r["insurance"]), Crew: txt(r["crew"]),
			StorageSCU: txt(r["storage_scu"]), FlightReady: txt(r["flight_ready"]), Notes: txt(r["notes"]), Image: txt(r["image"]),
			CustomName: txt(r["custom_name"]), MembershipID: txt(r["membership_id"]), SortOrder: num(r["sort_order"]),
			History: parseHistory(txt(r["upgrade_history"]))}
		pkg := h.pkgOf(it)
		// Ships it used to be are searchable too ("Golem" finds the Prospector it became), and so is who a gift went to.
		parts := []string{it.Name, it.CustomName, it.MembershipID, it.Type, it.Manufacturer, it.Purpose, it.Notes, pkg.Name, pkg.RSIPledgeID, pkg.GiftedTo}
		for _, s := range it.History {
			parts = append(parts, s.From, s.To)
		}
		it.spaced = searchText(strings.Join(parts, " | "))
		it.compact = strings.ReplaceAll(it.spaced, " ", "")
		h.Items = append(h.Items, it)
	}
	return h, nil
}

var noPackage = &Package{Status: "owned"}

func (h *Hangar) pkgOf(it *Item) *Package {
	if p := h.byID[it.PackageID]; p != nil {
		return p
	}
	return noPackage
}

func (h *Hangar) status(it *Item) string { return h.pkgOf(it).Status }

// ---------- search ----------

var searchJunk = regexp.MustCompile(`[^a-z0-9|]+`)

// searchText: "A.T.L.S" ≈ "atls", "CSV-SM" ≈ "csv sm", "Mk V" ≈ "mkv", accents and punctuation ignored.
func searchText(s string) string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(s) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(searchJunk.ReplaceAllString(strings.ToLower(b.String()), " "))
}

func queryTerms(q string) []string {
	out := []string{}
	for _, t := range strings.Split(searchText(q), " ") {
		if t != "" && t != "|" {
			out = append(out, t)
		}
	}
	return out
}

func matchesQuery(it *Item, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	all := true
	for _, t := range terms {
		if !strings.Contains(it.spaced, t) {
			all = false
			break
		}
	}
	return all || strings.Contains(it.compact, strings.Join(terms, ""))
}

// Melted, gifted and no-longer-in-hangar packages are kept but hidden from the normal lists.
type former struct{ label, past, nav string }

var formerStatuses = []string{"melted", "gifted", "missing"}
var formerInfo = map[string]former{
	"melted":  {"Melted", "melted", "Melted"},
	"gifted":  {"Gifted away", "gifted away", "Gifted away"},
	"missing": {"No longer in hangar", "no longer in your hangar", "Not in hangar"},
}

// statusMatches: "owned", one former status, "former" (any of them) or "any".
func (h *Hangar) statusMatches(it *Item, status string) bool {
	s := h.status(it)
	switch status {
	case "any":
		return true
	case "former":
		return s != "owned"
	}
	return s == status
}

const noPurpose = "No purpose"

// ---------- columns: each one sorts (click its title) and filters (the box under it) ----------

type Column struct {
	Key, Label, Kind string // kind: text | select | number ("" for the picture)
	Width            float32
	Right            bool
	Get              func(h *Hangar, it *Item) any // string, or float64 / nil for numbers
}

const blankChoice = "__blank__"

func numOrNil(s string) any {
	if f, ok := money(s); ok {
		return f
	}
	return nil
}

var columns = []Column{
	{Key: "image", Label: "", Width: 0},
	{Key: "name", Label: "Name", Kind: "text", Width: 280, Get: func(h *Hangar, it *Item) any { return it.Name }},
	{Key: "type", Label: "Type", Kind: "select", Width: 130, Get: func(h *Hangar, it *Item) any { return it.Type }},
	{Key: "manufacturer", Label: "Manufacturer", Kind: "select", Width: 150, Get: func(h *Hangar, it *Item) any { return it.Manufacturer }},
	{Key: "purpose", Label: "Purpose", Kind: "text", Width: 150, Get: func(h *Hangar, it *Item) any { return it.Purpose }},
	{Key: "insurance", Label: "Insurance", Kind: "select", Width: 110, Get: func(h *Hangar, it *Item) any { return it.Insurance }},
	{Key: "crew", Label: "Crew", Kind: "text", Width: 80, Get: func(h *Hangar, it *Item) any { return it.Crew }},
	{Key: "storage_scu", Label: "Storage", Kind: "number", Width: 90, Right: true, Get: func(h *Hangar, it *Item) any {
		if f, err := strconv.ParseFloat(strings.TrimSpace(it.StorageSCU), 64); err == nil {
			return f
		}
		return nil
	}},
	{Key: "flight_ready", Label: "Flight Ready", Kind: "select", Width: 110, Get: func(h *Hangar, it *Item) any { return it.FlightReady }},
	{Key: "pledge_date", Label: "Date", Kind: "text", Width: 105, Get: func(h *Hangar, it *Item) any { return h.pkgOf(it).PledgeDate }},
	{Key: "purchase_value", Label: "Purchase", Kind: "number", Width: 105, Right: true, Get: func(h *Hangar, it *Item) any { return numOrNil(h.pkgOf(it).PurchaseValue) }},
	{Key: "value", Label: "Value", Kind: "number", Width: 105, Right: true, Get: func(h *Hangar, it *Item) any { return numOrNil(h.pkgOf(it).Value) }},
	// RSI gives IDs to pledges (packages) and upgrades, not to the single ships inside a package; this is the ship's pledge.
	{Key: "pledge_id", Label: "Pledge ID", Kind: "text", Width: 115, Get: func(h *Hangar, it *Item) any { return h.pkgOf(it).RSIPledgeID }},
	// Ships you can rename do have an ID of their own.
	{Key: "membership_id", Label: "Ship ID", Kind: "text", Width: 115, Get: func(h *Hangar, it *Item) any { return it.MembershipID }},
}

func columnByKey(k string) *Column {
	for i := range columns {
		if columns[i].Key == k {
			return &columns[i]
		}
	}
	return nil
}

// numberFilter understands 100, >100, >=100, <50, <=50, 100-200 (a leading $ and commas are ignored). ok is false when
// the text isn't understood (the box turns red and the filter is ignored); test is nil for an empty box.
var (
	numCmpRe   = regexp.MustCompile(`^(>=|<=|>|<|=)?(\d+(?:\.\d+)?)$`)
	numRangeRe = regexp.MustCompile(`^(\d+(?:\.\d+)?)-(\d+(?:\.\d+)?)$`)
	numJunkRe  = regexp.MustCompile(`[$,\s]`)
)

func numberFilter(s string) (test func(any) bool, ok bool) {
	t := numJunkRe.ReplaceAllString(s, "")
	if t == "" {
		return nil, true
	}
	if m := numCmpRe.FindStringSubmatch(t); m != nil {
		n, _ := strconv.ParseFloat(m[2], 64)
		op := m[1]
		return func(v any) bool {
			f, isNum := v.(float64)
			if !isNum {
				return false
			}
			switch op {
			case ">":
				return f > n
			case "<":
				return f < n
			case ">=":
				return f >= n
			case "<=":
				return f <= n
			}
			return f == n
		}, true
	}
	if m := numRangeRe.FindStringSubmatch(t); m != nil {
		lo, _ := strconv.ParseFloat(m[1], 64)
		hi, _ := strconv.ParseFloat(m[2], 64)
		lo, hi = math.Min(lo, hi), math.Max(lo, hi)
		return func(v any) bool { f, isNum := v.(float64); return isNum && f >= lo && f <= hi }, true
	}
	return nil, false
}

func columnMatches(h *Hangar, it *Item, c *Column, value string) bool {
	v := c.Get(h, it)
	switch c.Kind {
	case "select":
		s, _ := v.(string)
		if value == blankChoice {
			return s == ""
		}
		return s == value
	case "number":
		if test, _ := numberFilter(value); test != nil {
			return test(v)
		}
		return true
	}
	s, _ := v.(string)
	return strings.Contains(strings.ToLower(s), strings.ToLower(value)) || strings.Contains(searchText(s), searchText(value))
}

// View is what the person has picked: which list, sidebar filter, search, column filters and sorting.
type View struct {
	Mode          string // items | packages
	Status        string // "" = what I own now; melted | gifted | missing
	Type, Purpose string
	Query         string
	Filters       map[string]string
	SortKey       string
	SortDir       int
	PkgSort       string
	PkgDir        int
}

func (h *Hangar) filteredItems(v *View, ignoreType, ignoreFilters bool, status string) []*Item {
	if status == "" {
		status = v.Status
		if status == "" {
			status = "owned"
		}
	}
	terms := queryTerms(v.Query)
	out := []*Item{}
	for _, it := range h.Items {
		if !h.statusMatches(it, status) {
			continue
		}
		if !ignoreType && v.Type != "" && orDefault(it.Type, "Other") != v.Type {
			continue
		}
		if !ignoreType && v.Purpose != "" && orDefault(it.Purpose, noPurpose) != v.Purpose {
			continue
		}
		if !ignoreFilters {
			ok := true
			for k, val := range v.Filters {
				if c := columnByKey(k); c != nil && c.Get != nil && !columnMatches(h, it, c, val) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
		}
		if matchesQuery(it, terms) {
			out = append(out, it)
		}
	}
	return out
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// listOrder is the default order: most recent purchase first (packages without a date last), then the order RSI
// lists them in.
func (h *Hangar) listLess(a, b *Item) bool {
	pa, pb := h.pkgOf(a), h.pkgOf(b)
	if pa.PledgeDate != pb.PledgeDate {
		if pa.PledgeDate == "" {
			return false
		}
		if pb.PledgeDate == "" {
			return true
		}
		return pa.PledgeDate > pb.PledgeDate
	}
	if pa.SortOrder != pb.SortOrder {
		return pa.SortOrder < pb.SortOrder
	}
	if a.PackageID != b.PackageID {
		return a.PackageID < b.PackageID
	}
	if a.SortOrder != b.SortOrder {
		return a.SortOrder < b.SortOrder
	}
	return a.ID < b.ID
}

func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && s == ""
}

func (h *Hangar) sortItems(items []*Item, v *View) []*Item {
	out := append([]*Item{}, items...)
	c := columnByKey(v.SortKey)
	if c == nil || c.Get == nil {
		sort.SliceStable(out, func(i, j int) bool { return h.listLess(out[i], out[j]) })
		return out
	}
	sort.SliceStable(out, func(i, j int) bool {
		av, bv := c.Get(h, out[i]), c.Get(h, out[j])
		if isEmpty(av) != isEmpty(bv) {
			return !isEmpty(av) // blanks last, whichever way you sort
		}
		if isEmpty(av) {
			return h.listLess(out[i], out[j])
		}
		var cmp int
		if c.Kind == "number" {
			d := av.(float64) - bv.(float64)
			cmp = int(math.Copysign(1, d))
			if d == 0 {
				cmp = 0
			}
		} else {
			cmp = naturalCompare(av.(string), bv.(string))
		}
		cmp *= v.SortDir
		if cmp != 0 {
			return cmp < 0
		}
		return h.listLess(out[i], out[j])
	})
	return out
}

var digitsRe = regexp.MustCompile(`\d+|\D+`)

// naturalCompare orders text the way people expect: case-insensitive, "Mk 2" before "Mk 10".
func naturalCompare(a, b string) int {
	pa, pb := digitsRe.FindAllString(strings.ToLower(a), -1), digitsRe.FindAllString(strings.ToLower(b), -1)
	for i := 0; i < len(pa) && i < len(pb); i++ {
		x, y := pa[i], pb[i]
		if x == y {
			continue
		}
		nx, ex := strconv.Atoi(x)
		ny, ey := strconv.Atoi(y)
		if ex == nil && ey == nil && nx != ny {
			if nx < ny {
				return -1
			}
			return 1
		}
		if x < y {
			return -1
		}
		return 1
	}
	switch {
	case len(pa) < len(pb):
		return -1
	case len(pa) > len(pb):
		return 1
	}
	return 0
}

type count struct {
	Name string
	N    int
}

func countBy(items []*Item, key func(*Item) string) []count {
	m := map[string]int{}
	for _, it := range items {
		m[key(it)]++
	}
	out := []count{}
	for k, n := range m {
		out = append(out, count{k, n})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (h *Hangar) owned() []*Item {
	out := []*Item{}
	for _, it := range h.Items {
		if h.status(it) == "owned" {
			out = append(out, it)
		}
	}
	return out
}

// ---------- money and upgrades ----------

// fmtMoney writes 1045 as "$1,045.00".
func fmtMoney(n float64) string {
	s := strconv.FormatFloat(math.Abs(n), 'f', 2, 64)
	whole, frac := s[:len(s)-3], s[len(s)-3:]
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	sign := ""
	if n < 0 {
		sign = "-"
	}
	return sign + "$" + b.String() + frac
}

func upgradeCost(items []*Item) float64 {
	total := 0.0
	for _, it := range items {
		for _, s := range it.History {
			if m, ok := money(s.Price); ok {
				total += m
			}
		}
	}
	return total
}

// placeholder: RSI shows no picture for hangars, digital downloads, name reservations or bodykit parts, so those get a
// symbol; everything else without a picture shows its first two letters.
var (
	hangarRe  = regexp.MustCompile(`hangar`)
	nameResRe = regexp.MustCompile(`name reservation`)
	digitalRe = regexp.MustCompile(`digital download|manual|starting money|\buec\b`)
	partRe    = regexp.MustCompile(`bodykit|module`)
)

func placeholder(it *Item) (string, bool) {
	n := strings.ToLower(it.Name)
	switch {
	case hangarRe.MatchString(n):
		return "⌂", true
	case nameResRe.MatchString(n):
		return "#", true
	case digitalRe.MatchString(n):
		return "◉", true
	case partRe.MatchString(n) || it.Type == "Component":
		return "✱", true
	}
	s := []rune(orDefault(orDefault(it.Type, it.Name), "?"))
	if len(s) > 2 {
		s = s[:2]
	}
	return strings.ToUpper(string(s)), false
}

// ownershipText answers "do I already own this?" for a search. It only counts what is in the hangar now; melted or
// gifted matches are mentioned separately. owned says whether the answer is good news.
func (h *Hangar) ownershipText(v *View) (msg string, owned bool) {
	q := strings.TrimSpace(v.Query)
	if q == "" {
		return "", false
	}
	all := h.filteredItems(v, true, true, "owned")
	gone := h.filteredItems(v, true, true, "former")
	ships := 0
	pkgs := map[int64]bool{}
	for _, it := range all {
		switch strings.ToLower(it.Type) {
		case "ship", "ground", "hover bike":
			ships++
		}
		pkgs[it.PackageID] = true
	}
	plural := func(n int, one, many string) string {
		if n == 1 {
			return one
		}
		return many
	}
	if len(all) == 0 {
		msg = fmt.Sprintf("Nothing in your hangar matches “%s”.", q)
	} else {
		shipNote := ""
		if ships > 0 {
			shipNote = fmt.Sprintf(" — including %d ship/vehicle%s", ships, plural(ships, "", "s"))
		}
		msg = fmt.Sprintf("✓ You already own %d item%s matching “%s”%s, across %d package%s.", len(all), plural(len(all), "", "s"), q,
			shipNote, len(pkgs), plural(len(pkgs), "", "s"))
	}
	if len(gone) > 0 {
		seen := map[int64]bool{}
		list := []string{}
		n := 0
		for _, it := range gone {
			if seen[it.PackageID] {
				continue
			}
			seen[it.PackageID] = true
			n++
			p := h.pkgOf(it)
			if len(list) < 3 {
				when := ""
				if p.StatusDate != "" {
					when = " " + p.StatusDate
				}
				list = append(list, fmt.Sprintf("%s (%s%s)", p.Name, formerInfo[p.Status].past, when))
			}
		}
		more := ""
		if n > 3 {
			more = "…"
		}
		msg += fmt.Sprintf(" You previously had %d matching item%s that %s no longer in your hangar: %s%s.", len(gone),
			plural(len(gone), "", "s"), plural(len(gone), "is", "are"), strings.Join(list, "; "), more)
	}
	return msg, len(all) > 0
}

// ---------- the packages view ----------

var pkgSorts = [][2]string{{"date", "Purchase date"}, {"name", "Name"}, {"purchase", "Purchase value"}, {"upgrades", "Upgrade cost"},
	{"value", "Current value"}, {"items", "Number of items"}}

func (h *Hangar) itemsByPackage() map[int64][]*Item {
	m := map[int64][]*Item{}
	for _, it := range h.Items {
		m[it.PackageID] = append(m[it.PackageID], it)
	}
	return m
}

func (h *Hangar) visiblePackages(v *View) []*Package {
	terms := queryTerms(v.Query)
	filtering := len(terms) > 0 || len(v.Filters) > 0
	match := map[int64]bool{}
	for _, it := range h.filteredItems(v, true, false, "") {
		match[it.ID] = true
	}
	byPkg := h.itemsByPackage()
	want := orDefault(v.Status, "owned")
	out := []*Package{}
	for _, p := range h.Packages {
		if p.Status != want {
			continue
		}
		if filtering {
			hit := false
			if len(terms) > 0 && len(v.Filters) == 0 {
				name := searchText(p.Name)
				hit = true
				for _, t := range terms {
					if !strings.Contains(name, t) {
						hit = false
						break
					}
				}
			}
			for _, it := range byPkg[p.ID] {
				if match[it.ID] {
					hit = true
				}
			}
			if !hit {
				continue
			}
		}
		out = append(out, p)
	}
	key := func(p *Package) any {
		switch v.PkgSort {
		case "name":
			return strings.ToLower(p.Name)
		case "date":
			return p.PledgeDate
		case "purchase":
			return numOrNil(p.PurchaseValue)
		case "value":
			return numOrNil(p.Value)
		case "upgrades":
			if c := upgradeCost(byPkg[p.ID]); c != 0 {
				return c
			}
			return nil
		case "items":
			return float64(len(byPkg[p.ID]))
		}
		return nil
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := key(out[i]), key(out[j])
		if isEmpty(a) != isEmpty(b) {
			return !isEmpty(a)
		}
		cmp := 0
		if !isEmpty(a) {
			if fa, ok := a.(float64); ok {
				fb := b.(float64)
				if fa < fb {
					cmp = -1
				} else if fa > fb {
					cmp = 1
				}
			} else {
				cmp = naturalCompare(a.(string), b.(string))
			}
			cmp *= v.PkgDir
		}
		if cmp != 0 {
			return cmp < 0
		}
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].ID < out[j].ID
	})
	return out
}
