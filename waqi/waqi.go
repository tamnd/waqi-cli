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
		UserAgent: "waqi-cli/0.1.0 (github.com/tamnd/waqi-cli)",
		Token:     "demo",
		Rate:      200 * time.Millisecond,
		Timeout:   30 * time.Second,
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
type Station struct {
	IDX         int     `json:"idx"`
	City        string  `json:"city"`
	CityURL     string  `json:"city_url"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	AQI         int     `json:"aqi"`
	DominantPol string  `json:"dominant_pol"`
	Time        string  `json:"time"`
	TZ          string  `json:"tz"`
	PM25        float64 `json:"pm25"`
	PM10        float64 `json:"pm10"`
	NO2         float64 `json:"no2"`
	O3          float64 `json:"o3"`
	CO          float64 `json:"co"`
	SO2         float64 `json:"so2"`
	Temp        float64 `json:"temp"`
	Humidity    float64 `json:"humidity"`
	Pressure    float64 `json:"pressure"`
	Dew         float64 `json:"dew"`
	Wind        float64 `json:"wind"`
}

// SearchResult is one entry returned by the search endpoint.
type SearchResult struct {
	UID  int     `json:"uid"`
	AQI  string  `json:"aqi"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
	URL  string  `json:"url"`
	Time string  `json:"time"`
}

// --- wire types ---

type iaqiVal struct {
	V float64 `json:"v"`
}

type feedResponse struct {
	Status string `json:"status"`
	Data   json.RawMessage `json:"data"`
}

type feedData struct {
	IDX  int `json:"idx"`
	AQI  int `json:"aqi"`
	Time struct {
		S  string `json:"s"`
		TZ string `json:"tz"`
	} `json:"time"`
	City struct {
		Name string    `json:"name"`
		Geo  []float64 `json:"geo"`
		URL  string    `json:"url"`
	} `json:"city"`
	DominentPol string `json:"dominentpol"`
	IAQI        struct {
		Dew  *iaqiVal `json:"dew"`
		H    *iaqiVal `json:"h"`
		NO2  *iaqiVal `json:"no2"`
		O3   *iaqiVal `json:"o3"`
		P    *iaqiVal `json:"p"`
		PM10 *iaqiVal `json:"pm10"`
		PM25 *iaqiVal `json:"pm25"`
		CO   *iaqiVal `json:"co"`
		SO2  *iaqiVal `json:"so2"`
		T    *iaqiVal `json:"t"`
		W    *iaqiVal `json:"w"`
	} `json:"iaqi"`
}

type searchResponse struct {
	Status string `json:"status"`
	Data   json.RawMessage `json:"data"`
}

type searchEntry struct {
	UID  int    `json:"uid"`
	AQI  string `json:"aqi"`
	Time struct {
		STime string `json:"stime"`
	} `json:"time"`
	Station struct {
		Name string    `json:"name"`
		Geo  []float64 `json:"geo"`
		URL  string    `json:"url"`
	} `json:"station"`
}

// Feed returns real-time air quality data for the given city slug, station UID
// (e.g. "@1234"), or geographic coordinates (e.g. "geo:35.68;139.77").
func (c *Client) Feed(ctx context.Context, city string) (*Station, error) {
	rawURL := c.cfg.BaseURL + "/feed/" + url.PathEscape(city) + "/?token=" + c.cfg.Token
	b, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	var resp feedResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("decode feed response: %w", err)
	}
	if resp.Status != "ok" {
		// data is a string on error
		var msg string
		_ = json.Unmarshal(resp.Data, &msg)
		if msg == "" {
			msg = "unknown error"
		}
		return nil, fmt.Errorf("waqi: %s", msg)
	}

	var d feedData
	if err := json.Unmarshal(resp.Data, &d); err != nil {
		return nil, fmt.Errorf("decode feed data: %w", err)
	}

	s := &Station{
		IDX:         d.IDX,
		City:        d.City.Name,
		CityURL:     d.City.URL,
		AQI:         d.AQI,
		DominantPol: d.DominentPol,
		Time:        d.Time.S,
		TZ:          d.Time.TZ,
	}
	if len(d.City.Geo) >= 2 {
		s.Lat = d.City.Geo[0]
		s.Lng = d.City.Geo[1]
	}
	if d.IAQI.PM25 != nil {
		s.PM25 = d.IAQI.PM25.V
	}
	if d.IAQI.PM10 != nil {
		s.PM10 = d.IAQI.PM10.V
	}
	if d.IAQI.NO2 != nil {
		s.NO2 = d.IAQI.NO2.V
	}
	if d.IAQI.O3 != nil {
		s.O3 = d.IAQI.O3.V
	}
	if d.IAQI.CO != nil {
		s.CO = d.IAQI.CO.V
	}
	if d.IAQI.SO2 != nil {
		s.SO2 = d.IAQI.SO2.V
	}
	if d.IAQI.T != nil {
		s.Temp = d.IAQI.T.V
	}
	if d.IAQI.H != nil {
		s.Humidity = d.IAQI.H.V
	}
	if d.IAQI.P != nil {
		s.Pressure = d.IAQI.P.V
	}
	if d.IAQI.Dew != nil {
		s.Dew = d.IAQI.Dew.V
	}
	if d.IAQI.W != nil {
		s.Wind = d.IAQI.W.V
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

	var resp searchResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	if resp.Status != "ok" {
		var msg string
		_ = json.Unmarshal(resp.Data, &msg)
		if msg == "" {
			msg = "unknown error"
		}
		return nil, fmt.Errorf("waqi: %s", msg)
	}

	var entries []searchEntry
	if err := json.Unmarshal(resp.Data, &entries); err != nil {
		return nil, fmt.Errorf("decode search data: %w", err)
	}

	out := make([]SearchResult, 0, len(entries))
	for _, e := range entries {
		r := SearchResult{
			UID:  e.UID,
			AQI:  e.AQI,
			Name: e.Station.Name,
			URL:  e.Station.URL,
			Time: e.Time.STime,
		}
		if len(e.Station.Geo) >= 2 {
			r.Lat = e.Station.Geo[0]
			r.Lng = e.Station.Geo[1]
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

// iaqiFloat extracts a float from an optional iaqi value pointer.
func iaqiFloat(v *iaqiVal) float64 {
	if v == nil {
		return 0
	}
	return v.V
}
