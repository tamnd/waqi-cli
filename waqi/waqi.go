// Package waqi is the library behind the waqi command line:
// the HTTP client, request shaping, and typed data models for api.waqi.info.
//
// The Client sets a real User-Agent, paces requests, and retries transient
// failures (429 and 5xx) with exponential backoff. Two operations are provided:
// get real-time air quality for a city (Feed) and search stations by keyword.
package waqi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"
)

// Host is the site this client talks to.
const Host = "api.waqi.info"

// Config holds tunable knobs for the HTTP client.
type Config struct {
	BaseURL   string
	UserAgent string
	Token     string // default "demo"
	Rate      time.Duration
	Timeout   time.Duration
	Retries   int
}

// DefaultConfig returns sensible defaults for production use.
func DefaultConfig() Config {
	return Config{
		BaseURL:   "https://api.waqi.info",
		UserAgent: "waqi-cli/0.1 (tamnd87@gmail.com)",
		Token:     "demo",
		Rate:      500 * time.Millisecond,
		Timeout:   10 * time.Second,
		Retries:   3,
	}
}

// Client talks to api.waqi.info over HTTP.
type Client struct {
	cfg  Config
	http *http.Client
	mu   sync.Mutex
	last time.Time
}

// NewClient returns a Client configured with cfg.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
	}
}

// Station holds the parsed air quality data for one monitoring station.
// String fields use "%.1f" formatting or "-" when the measurement is absent.
type Station struct {
	City     string `kit:"id" json:"city"`
	AQI      int    `json:"aqi"`
	Dominant string `json:"dominant_pollutant"` // dominentpol field
	PM25     string `json:"pm25"`               // iaqi.pm25.v, formatted "%.1f" or "-"
	PM10     string `json:"pm10"`               // iaqi.pm10.v
	O3       string `json:"o3"`                 // iaqi.o3.v
	NO2      string `json:"no2"`                // iaqi.no2.v
	SO2      string `json:"so2"`                // iaqi.so2.v
	Temp     string `json:"temp_c"`             // iaqi.t.v
	Updated  string `json:"updated"`            // time.s
}

// SearchResult is one entry returned by the search endpoint.
type SearchResult struct {
	UID  int    `kit:"id" json:"uid"`
	Name string `json:"name"`
	AQI  string `json:"aqi"`
	Lat  string `json:"lat"`
	Lon  string `json:"lon"`
}

// --- wire types ---

type wireIaqi struct {
	PM25 *struct {
		V float64 `json:"v"`
	} `json:"pm25"`
	PM10 *struct {
		V float64 `json:"v"`
	} `json:"pm10"`
	O3 *struct {
		V float64 `json:"v"`
	} `json:"o3"`
	NO2 *struct {
		V float64 `json:"v"`
	} `json:"no2"`
	SO2 *struct {
		V float64 `json:"v"`
	} `json:"so2"`
	T *struct {
		V float64 `json:"v"`
	} `json:"t"`
}

type wireStation struct {
	AQI         int    `json:"aqi"`
	DominentPol string `json:"dominentpol"`
	City        struct {
		Name string    `json:"name"`
		Geo  []float64 `json:"geo"`
	} `json:"city"`
	Iaqi wireIaqi `json:"iaqi"`
	Time struct {
		S string `json:"s"`
	} `json:"time"`
}

type wireResponse struct {
	Status string      `json:"status"`
	Data   wireStation `json:"data"`
}

// wireResponseRaw is used for error extraction (data is a string on error).
type wireResponseRaw struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
}

type wireSearchItem struct {
	UID  int    `json:"uid"`
	AQI  string `json:"aqi"` // note: string in search results!
	Time struct {
		STime string `json:"stime"`
	} `json:"time"`
	Station struct {
		Name string    `json:"name"`
		Geo  []float64 `json:"geo"`
	} `json:"station"`
}

type wireSearchResponse struct {
	Status string           `json:"status"`
	Data   []wireSearchItem `json:"data"`
}

// reLookupGeo matches "lat,lon" patterns so Feed can route them as geo lookups.
var reLookupGeo = regexp.MustCompile(`^\d+\.\d+,-?\d+\.\d+$`)

// fmtF formats an optional float as "%.1f" or "-" when nil.
func fmtF(v *struct {
	V float64 `json:"v"`
}) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f", v.V)
}

// Feed returns real-time air quality data for the given location.
// location can be a city name ("beijing"), a station UID ("@8076"),
// or "lat,lon" coordinates ("48.8566,2.3522").
func (c *Client) Feed(ctx context.Context, location string) (*Station, error) {
	var path string
	if reLookupGeo.MatchString(location) {
		// Convert "lat,lon" to "geo:lat;lon"
		// Replace the comma separator with semicolon for the API
		for i, ch := range location {
			if ch == ',' {
				path = "geo:" + location[:i] + ";" + location[i+1:]
				break
			}
		}
	} else {
		path = location
	}

	rawURL := c.cfg.BaseURL + "/feed/" + url.PathEscape(path) + "/?token=" + c.cfg.Token
	b, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	// First try to detect error status.
	var raw wireResponseRaw
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("decode feed response: %w", err)
	}
	if raw.Status != "ok" {
		var msg string
		_ = json.Unmarshal(raw.Data, &msg)
		if msg == "" {
			msg = "unknown error"
		}
		return nil, fmt.Errorf("waqi: %s", msg)
	}

	var resp wireResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("decode feed data: %w", err)
	}
	d := resp.Data

	s := &Station{
		City:     d.City.Name,
		AQI:      d.AQI,
		Dominant: d.DominentPol,
		PM25:     fmtF(d.Iaqi.PM25),
		PM10:     fmtF(d.Iaqi.PM10),
		O3:       fmtF(d.Iaqi.O3),
		NO2:      fmtF(d.Iaqi.NO2),
		SO2:      fmtF(d.Iaqi.SO2),
		Temp:     fmtF(d.Iaqi.T),
		Updated:  d.Time.S,
	}
	return s, nil
}

// Search searches for monitoring stations matching the keyword.
func (c *Client) Search(ctx context.Context, keyword string) ([]SearchResult, error) {
	rawURL := c.cfg.BaseURL + "/search/?keyword=" + url.QueryEscape(keyword) + "&token=" + c.cfg.Token
	b, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	var resp wireSearchResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	if resp.Status != "ok" {
		return nil, fmt.Errorf("waqi: unknown error")
	}

	out := make([]SearchResult, 0, len(resp.Data))
	for _, e := range resp.Data {
		r := SearchResult{
			UID:  e.UID,
			AQI:  e.AQI,
			Name: e.Station.Name,
		}
		if len(e.Station.Geo) >= 2 {
			r.Lat = fmt.Sprintf("%.4f", e.Station.Geo[0])
			r.Lon = fmt.Sprintf("%.4f", e.Station.Geo[1])
		}
		out = append(out, r)
	}
	return out, nil
}

// get fetches url and returns the response body. It paces and retries.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) ([]byte, bool, error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfg.Rate <= 0 {
		return
	}
	if wait := c.cfg.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}
