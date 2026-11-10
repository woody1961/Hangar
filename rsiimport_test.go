package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNamesAreComparedWithoutPunctuation(t *testing.T) {
	if got := normText("A.T.L.S — Café"); got != "a t l s cafe" {
		t.Fatalf("normText = %q", got)
	}
	cases := []struct {
		a, b string
		want int
	}{
		{"Ursa", "Ursa Medivac", 1},
		{"CSV-SM", "csv sm", 2},
		{"X1 Force (Upgraded)", "X1 Force", 2},
		{"Cutter", "Nomad", 0},
	}
	for _, c := range cases {
		if got := matchLevel(c.a, c.b); got != c.want {
			t.Errorf("matchLevel(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestLabelsAndAmounts(t *testing.T) {
	for in, want := range map[string]string{"Lifetime Insurance": "Lifetime", "120 Month Insurance": "120 months",
		"10 Year Insurance": "10 years", "1 month insurance": "1 month"} {
		if got := insuranceLabel(in); got != want {
			t.Errorf("insuranceLabel(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"Apollo Medivac Warbond Edition": "Apollo Medivac", "Warbond Edition": "", "X1 Force Standard Edition": "X1 Force"} {
		if got := cleanTo(in); got != want {
			t.Errorf("cleanTo(%q) = %q, want %q", in, got, want)
		}
	}
	if n, ok := money("$1,045.00 USD"); !ok || n != 1045 {
		t.Errorf("money = %v %v", n, ok)
	}
	if _, ok := money("free"); ok {
		t.Error("money found an amount in text without one")
	}
	if got := parseCreated("Created: October 14, 2026"); got != "2026-10-14" {
		t.Errorf("parseCreated = %v", got)
	}
}

func TestLogLinesAreReadOldestFirst(t *testing.T) {
	var raw any
	json.Unmarshal([]byte(`[
	  "Sep 26 2025, 5:28 pm - Standalone Ship - HoverQuad #222 - Upgrade applied: #900 Upgrade - X1 Force to Apollo Medivac Warbond Edition, new value: $270.00 USD",
	  "Sep 20 2025, 5:28 pm - Standalone Ship - HoverQuad #222 - Upgrade applied: #899 Upgrade - HoverQuad to X1 Force Standard Edition, new value: $70.00 USD",
	  "Sep 20 2025, 5:28 pm - Standalone Ship - HoverQuad #222 - Upgrade applied: #899 Upgrade - HoverQuad to X1 Force Standard Edition, new value: $70.00 USD"
	]`), &raw)
	steps := parseUpgrades(raw)
	if len(steps) != 2 || steps[0].UpgradeID != "899" || steps[0].Date != "2025-09-20T17:28" || steps[1].To != "Apollo Medivac" {
		t.Fatalf("steps = %+v %+v", steps[0], steps[len(steps)-1])
	}
	chains := buildChains(steps)
	if len(chains) != 1 || len(chains[0]) != 2 {
		t.Fatalf("chains = %d", len(chains))
	}

	json.Unmarshal([]byte(`[
	  "Mar 03 2022, 7:32 pm - Package - Nomad Combo #333 - Gifted to friend@example.com, value: $96.00 USD",
	  "Nov 26 2021, 1:00 pm - Package - Nomad Combo #333 - Created by Pilot - order #DDD, value: $96.00 USD",
	  "Jan 02 2020, 1:00 pm - Something gone #555 - Reclaimed by Pilot for $50.00 USD",
	  "Dec 01 2019, 1:00 pm - A reward #666 - Star Citizen Team"
	]`), &raw)
	events := parseLifecycle(raw)
	kinds := []string{}
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, ",") != "melted,created,gifted" || events[2].To != "friend@example.com" {
		t.Fatalf("events = %v", kinds)
	}
}

func TestJunkPledgesAreRefused(t *testing.T) {
	var raw any
	json.Unmarshal([]byte(`[{"id": "abc", "name": "x"}, {"id": "12", "name": ""}]`), &raw)
	if _, err := cleanPledges(raw); err == nil {
		t.Fatal("junk was accepted")
	}
	dec := json.NewDecoder(strings.NewReader(`[{"id": 111, "name": " Cutter ", "items": [
	  {"title": "Cutter", "kind": "Ship", "image": "https://evil.example.com/x.jpg"},
	  {"title": "Cutter", "kind": "Ship", "image": "https://media.robertsspaceindustries.com/x.jpg"}]},
	  {"id": "111", "name": "again"}]`))
	dec.UseNumber()
	dec.Decode(&raw)
	pledges, err := cleanPledges(raw)
	if err != nil || len(pledges) != 1 || pledges[0].ID != "111" || pledges[0].Name != "Cutter" {
		t.Fatalf("pledges = %v %v", pledges, err)
	}
	if pledges[0].Items[0].Image != "" || pledges[0].Items[1].Image == "" {
		t.Fatal("only RSI's own pictures may be downloaded")
	}
}
