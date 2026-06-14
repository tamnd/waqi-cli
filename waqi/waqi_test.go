package waqi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/waqi-cli/waqi"
)

func newTestClient(ts *httptest.Server) *waqi.Client {
	cfg := waqi.DefaultConfig()
	cfg.BaseURL = ts.URL
	cfg.Rate = 0
	return waqi.NewClient(cfg)
}

const mockFeedResponse = `{
  "status": "ok",
  "data": {
    "idx": 1437,
    "aqi": 70,
    "time": {"s": "2026-06-14 13:00:00", "tz": "+09:00", "v": 1749902400},
    "city": {"name": "Tokyo", "geo": [35.6826, 139.769], "url": "https://aqicn.org/city/tokyo/"},
    "dominentpol": "pm25",
    "iaqi": {
      "no2":  {"v": 2.3},
      "pm10": {"v": 22},
      "pm25": {"v": 70},
      "t":    {"v": 22}
    }
  }
}`

const mockSearchResponse = `{
  "status": "ok",
  "data": [
    {
      "uid": 1437,
      "aqi": "70",
      "time": {"stime": "2026-06-14 13:00:00", "tz": "+09:00", "vtime": 1749902400},
      "station": {
        "name": "Shinjuku, Tokyo",
        "geo": [35.68, 139.77],
        "url": "https://aqicn.org/station/@1437/"
      }
    }
  ]
}`

const mockErrorResponse = `{"status": "error", "data": "Unknown station"}`

// TestFeedSendsUserAgent asserts every request carries a User-Agent header.
func TestFeedSendsUserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get("User-Agent")
		if ua == "" {
			t.Error("request carried no User-Agent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockFeedResponse)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.Feed(context.Background(), "tokyo")
	if err != nil {
		t.Fatal(err)
	}
}

// TestFeedParsesAQIAndDominantPol verifies core fields are parsed from the feed.
func TestFeedParsesAQIAndDominantPol(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockFeedResponse)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	s, err := c.Feed(context.Background(), "tokyo")
	if err != nil {
		t.Fatal(err)
	}
	if s.AQI != 70 {
		t.Errorf("AQI = %d, want 70", s.AQI)
	}
	if s.Dominant != "pm25" {
		t.Errorf("Dominant = %q, want %q", s.Dominant, "pm25")
	}
	if s.City != "Tokyo" {
		t.Errorf("City = %q, want %q", s.City, "Tokyo")
	}
	if s.Updated != "2026-06-14 13:00:00" {
		t.Errorf("Updated = %q, want %q", s.Updated, "2026-06-14 13:00:00")
	}
}

// TestFeedFormatsOptionalFields verifies formatted string fields.
func TestFeedFormatsOptionalFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockFeedResponse)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	s, err := c.Feed(context.Background(), "tokyo")
	if err != nil {
		t.Fatal(err)
	}
	if s.PM25 != "70.0" {
		t.Errorf("PM25 = %q, want %q", s.PM25, "70.0")
	}
	if s.PM10 != "22.0" {
		t.Errorf("PM10 = %q, want %q", s.PM10, "22.0")
	}
	if s.NO2 != "2.3" {
		t.Errorf("NO2 = %q, want %q", s.NO2, "2.3")
	}
	if s.O3 != "-" {
		t.Errorf("O3 = %q, want %q (absent)", s.O3, "-")
	}
	if s.SO2 != "-" {
		t.Errorf("SO2 = %q, want %q (absent)", s.SO2, "-")
	}
	if s.Temp != "22.0" {
		t.Errorf("Temp = %q, want %q", s.Temp, "22.0")
	}
}

// TestSearchParsesUIDAndName verifies search result fields.
func TestSearchParsesUIDAndName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockSearchResponse)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	results, err := c.Search(context.Background(), "tokyo")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].UID != 1437 {
		t.Errorf("UID = %d, want 1437", results[0].UID)
	}
	if results[0].Name != "Shinjuku, Tokyo" {
		t.Errorf("Name = %q, want %q", results[0].Name, "Shinjuku, Tokyo")
	}
	if results[0].AQI != "70" {
		t.Errorf("AQI = %q, want %q", results[0].AQI, "70")
	}
	if results[0].Lat != "35.6800" {
		t.Errorf("Lat = %q, want %q", results[0].Lat, "35.6800")
	}
	if results[0].Lon != "139.7700" {
		t.Errorf("Lon = %q, want %q", results[0].Lon, "139.7700")
	}
}

// TestFeedGeoLookup verifies that "lat,lon" input generates a geo: path.
func TestFeedGeoLookup(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockFeedResponse)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.Feed(context.Background(), "48.8566,2.3522")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotPath, "geo:") {
		t.Errorf("path = %q, want to contain geo:", gotPath)
	}
}

// TestFeedRetriesOn503 verifies that the client retries on 5xx responses.
func TestFeedRetriesOn503(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockFeedResponse)
	}))
	defer srv.Close()

	cfg := waqi.DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 5
	c := waqi.NewClient(cfg)

	start := time.Now()
	s, err := c.Feed(context.Background(), "tokyo")
	if err != nil {
		t.Fatal(err)
	}
	if s.AQI != 70 {
		t.Errorf("AQI = %d, want 70 after retries", s.AQI)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

// TestFeedErrorStatusReturnsError verifies that status=error yields a non-nil error.
func TestFeedErrorStatusReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, mockErrorResponse)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.Feed(context.Background(), "unknowncityxyz")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Unknown station") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "Unknown station")
	}
}
