package waqi

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the domain driver's wiring, which
// need no network. The client's HTTP behaviour is covered in waqi_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "waqi" {
		t.Errorf("Scheme = %q, want waqi", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "waqi" {
		t.Errorf("Identity.Binary = %q, want waqi", info.Identity.Binary)
	}
}

// TestHostWiring mounts the driver in a kit Host and checks the domain is
// registered and can be looked up.
func TestHostWiring(t *testing.T) {
	_, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	dom, ok := kit.Lookup("waqi")
	if !ok {
		t.Fatal("domain waqi not registered")
	}
	info := dom.Info()
	if info.Scheme != "waqi" {
		t.Errorf("Scheme = %q, want waqi", info.Scheme)
	}
}

// TestStationFieldTypes verifies Station uses string for formatted measurements.
func TestStationFieldTypes(t *testing.T) {
	s := Station{
		City:     "Tokyo",
		AQI:      70,
		Dominant: "pm25",
		PM25:     "70.0",
		PM10:     "22.0",
		O3:       "-",
		NO2:      "2.3",
		SO2:      "-",
		Temp:     "22.0",
		Updated:  "2026-06-15 00:00:00",
	}
	if s.City == "" {
		t.Error("City should not be empty")
	}
	if s.PM25 != "70.0" {
		t.Errorf("PM25 = %q, want %q", s.PM25, "70.0")
	}
	if s.O3 != "-" {
		t.Errorf("O3 should be dash when absent, got %q", s.O3)
	}
}

// TestSearchResultFieldTypes verifies SearchResult uses string for coordinates.
func TestSearchResultFieldTypes(t *testing.T) {
	r := SearchResult{
		UID:  1437,
		Name: "Shinjuku, Tokyo",
		AQI:  "70",
		Lat:  "35.6800",
		Lon:  "139.7700",
	}
	if r.UID != 1437 {
		t.Errorf("UID = %d, want 1437", r.UID)
	}
	if r.Lat != "35.6800" {
		t.Errorf("Lat = %q, want %q", r.Lat, "35.6800")
	}
}
