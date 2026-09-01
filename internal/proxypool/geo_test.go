package proxypool

import (
	"strings"
	"testing"
	"time"
)

func TestGeoCache_RecordHistoryCap(t *testing.T) {
	g := NewGeoCache()
	// 12 distinct IPs over time; history must be capped at geoHistoryCap (8)
	// keeping only the most recent 8.
	for i := 0; i < 12; i++ {
		ip := "1.2.3." + itoa(i)
		g.Record("pool-1", ip, "US", "CA", "San Jose", "Example LLC", time.Now())
	}
	entry, ok := g.Get("pool-1")
	if !ok {
		t.Fatal("expected entry for pool-1")
	}
	if len(entry.IPHistory) != geoHistoryCap {
		t.Fatalf("history length = %d, want %d", len(entry.IPHistory), geoHistoryCap)
	}
	// Most recent 8: indices 4..11
	if got, want := entry.IPHistory[0], "1.2.3.4"; got != want {
		t.Fatalf("oldest kept ip = %s, want %s", got, want)
	}
	if got, want := entry.IPHistory[geoHistoryCap-1], "1.2.3.11"; got != want {
		t.Fatalf("newest kept ip = %s, want %s", got, want)
	}
}

func TestGeoCache_RecordDedupesConsecutive(t *testing.T) {
	g := NewGeoCache()
	now := time.Now()
	// Same IP repeated consecutively must not grow the history.
	g.Record("pool-1", "9.9.9.9", "US", "", "", "", now)
	g.Record("pool-1", "9.9.9.9", "US", "", "", "", now)
	g.Record("pool-1", "9.9.9.9", "US", "", "", "", now)
	entry, _ := g.Get("pool-1")
	if len(entry.IPHistory) != 1 {
		t.Fatalf("history length = %d, want 1 (consecutive dedupe)", len(entry.IPHistory))
	}
	if entry.IsUnstable {
		t.Fatal("single stable IP must not be flagged unstable")
	}
}

func TestGeoCache_RecordUnstableOnTwoIPs(t *testing.T) {
	g := NewGeoCache()
	now := time.Now()
	g.Record("pool-1", "1.1.1.1", "US", "", "", "", now)
	if entry, _ := g.Get("pool-1"); entry.IsUnstable {
		t.Fatal("one IP must not be unstable")
	}
	g.Record("pool-1", "2.2.2.2", "US", "", "", "", now)
	if entry, _ := g.Get("pool-1"); !entry.IsUnstable {
		t.Fatal("two distinct egress IPs must be flagged unstable")
	}
	// A third distinct IP keeps it unstable and updates the history.
	g.Record("pool-1", "3.3.3.3", "US", "", "", "", now)
	entry, _ := g.Get("pool-1")
	if !entry.IsUnstable {
		t.Fatal("three distinct egress IPs must be flagged unstable")
	}
	if len(entry.IPHistory) != 3 {
		t.Fatalf("history length = %d, want 3", len(entry.IPHistory))
	}
}

func TestGeoCache_RecordErrorPreservesData(t *testing.T) {
	g := NewGeoCache()
	now := time.Now()
	g.Record("pool-1", "1.1.1.1", "DE", "Berlin", "Berlin", "Stadtnetz", now)
	g.RecordError("pool-1", "all endpoints unreachable", now)
	entry, ok := g.Get("pool-1")
	if !ok {
		t.Fatal("expected entry")
	}
	if entry.IP != "1.1.1.1" || entry.Country != "DE" {
		t.Fatalf("error must not clobber last geo: got ip=%s country=%s", entry.IP, entry.Country)
	}
	if entry.LastError != "all endpoints unreachable" {
		t.Fatalf("LastError = %q", entry.LastError)
	}
}

func TestGeoCache_SnapshotCopies(t *testing.T) {
	g := NewGeoCache()
	g.Record("pool-1", "1.1.1.1", "US", "", "", "", time.Now())
	g.Record("pool-2", "2.2.2.2", "JP", "", "", "", time.Now())
	snap := g.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
	// Mutating the snapshot must not affect the cache.
	e := snap["pool-1"]
	e.IPHistory = append(e.IPHistory, "evil")
	snap["pool-1"] = e
	entry, _ := g.Get("pool-1")
	if len(entry.IPHistory) != 1 {
		t.Fatalf("snapshot must be a deep copy, cache history len = %d", len(entry.IPHistory))
	}
}

func TestIsDatacenterOrg(t *testing.T) {
	cases := []struct {
		org  string
		want bool
	}{
		{"AS15169 Google LLC", true},
		{"Amazon Technologies Inc.", true},
		{"Microsoft Corporation", true},
		{"DigitalOcean, LLC", true},
		{"Hetzner Online GmbH", true},
		{"OVH SAS", true},
		{"Linode, LLC", true},
		{"Vultr Holdings, LLC", true},
		{"Cloudflare, Inc.", true},
		{"Oracle Corporation", true},
		{"Alibaba (US) Technology Co., Ltd.", true},
		{"Tencent Building, Kejizhongyi Avenue", true},
		{"Contabo GmbH", true},
		{"Scaleway", true},
		{"Leaseweb USA, Inc.", true},
		{"Datacamp Limited", true},
		{"Hosting Services, Inc.", true},
		{"Residential ISP Customer", false},
		{"Comcast Cable Communications", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isDatacenterOrg(c.org); got != c.want {
			t.Errorf("isDatacenterOrg(%q) = %v, want %v", c.org, got, c.want)
		}
	}
}

func TestParseGeo(t *testing.T) {
	cases := []struct {
		name                string
		body                string
		wantIP, wantCountry string
		wantOrg             string
		wantOK              bool
	}{
		{
			name: "ifconfig.co",
			body: `{"ip":"185.199.108.153","ip_decimal":3117454489,"country":"US","country_iso":"US","country_eu":false,"region_name":"California","city_name":"San Francisco","latitude":37.751,"longitude":-122.4247,"asn_org":"GitHub, Inc.","asn":"AS36459"}`,
			wantIP: "185.199.108.153", wantCountry: "US", wantOrg: "GitHub, Inc.", wantOK: true,
		},
		{
			name: "ipwho.is nested connection.org",
			body: `{"ip":"1.1.1.1","success":true,"type":"IPv4","connection":{"asn":13335,"org":"Cloudflare, Inc.","isp":"Cloudflare, Inc."},"country":"United States","country_code":"US","region":"California","city":"Los Angeles"}`,
			wantIP: "1.1.1.1", wantCountry: "United States", wantOrg: "Cloudflare, Inc.", wantOK: true,
		},
		{
			name: "ip-api.com isp/org",
			body: `{"query":"8.8.8.8","status":"success","country":"United States","countryCode":"US","regionName":"California","city":"Mountain View","isp":"Google LLC","org":"Google LLC","as":"AS15169 Google LLC"}`,
			wantIP: "8.8.8.8", wantCountry: "United States", wantOrg: "Google LLC", wantOK: true,
		},
		{
			name: "ipapi.co country_name",
			body: `{"ip":"104.16.132.229","city":"San Francisco","region":"California","country":"US","country_name":"United States","postal":"94107","latitude":37.7757,"longitude":-122.3952,"timezone":"America/Los_Angeles","asn":"AS13335","org":"Cloudflare, Inc.","country_code":"US","currency":"USD"}`,
			wantIP: "104.16.132.229", wantCountry: "US", wantOrg: "Cloudflare, Inc.", wantOK: true,
		},
		{
			name: "ipinfo.io org",
			body: `{"ip":"151.101.1.140","hostname":"edgecastcdn.net","city":"San Francisco","region":"California","country":"US","loc":"37.7749,-122.4194","org":"AS15169 Google LLC","postal":"94107","timezone":"America/Los_Angeles"}`,
			wantIP: "151.101.1.140", wantCountry: "US", wantOrg: "AS15169 Google LLC", wantOK: true,
		},
		{
			name: "non-json",
			body: `<html>gateway error</html>`,
			wantOK: false,
		},
		{
			name: "json without ip",
			body: `{"hello":"world"}`,
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ip, country, _, _, org, ok := parseGeo([]byte(c.body))
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if !ok {
				return
			}
			if ip != c.wantIP {
				t.Errorf("ip = %q, want %q", ip, c.wantIP)
			}
			if country != c.wantCountry {
				t.Errorf("country = %q, want %q", country, c.wantCountry)
			}
			if org != c.wantOrg {
				t.Errorf("org = %q, want %q", org, c.wantOrg)
			}
		})
	}
}

func TestGeoEndpointsForOverride(t *testing.T) {
	t.Setenv("GEO_PROBE_URL", "https://geo.example.test/json")
	got := geoEndpointsFor()
	if len(got) != 1 || got[0] != "https://geo.example.test/json" {
		t.Fatalf("override endpoints = %v, want [https://geo.example.test/json]", got)
	}

	t.Setenv("GEO_PROBE_URL", "")
	got = geoEndpointsFor()
	if len(got) != len(geoEndpoints) {
		t.Fatalf("default endpoints len = %d, want %d", len(got), len(geoEndpoints))
	}
	for i, e := range geoEndpoints {
		if got[i] != e {
			t.Fatalf("endpoint %d = %s, want %s", i, got[i], e)
		}
	}
}

func TestGeoHTTPErrorMessage(t *testing.T) {
	err := &geoHTTPError{status: 429, body: "rate limited"}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("geoHTTPError message %q must contain status", err.Error())
	}
}
