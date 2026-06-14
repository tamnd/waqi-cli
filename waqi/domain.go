// domain.go exposes waqi as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/waqi-cli/waqi"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// waqi:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone waqi binary (see cmd/waqi), so the
// binary and a host share one source of truth.
package waqi

import (
	"context"
	"os"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

func init() { kit.Register(Domain{}) }

// Domain is the waqi driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "waqi",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "waqi",
			Short:  "A command line for the World Air Quality Index.",
			Long: `waqi fetches real-time air quality data from api.waqi.info.

It returns the AQI, dominant pollutant, individual pollutant measurements
(PM2.5, PM10, NO2, O3, CO, SO2), and meteorological data for any city worldwide.
The demo token works for major cities; set WAQI_TOKEN to use a real token.`,
			Site: Host,
			Repo: "https://github.com/tamnd/waqi-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{
		Name: "feed", Group: "read", Single: true,
		Summary: "Get air quality for a city or station",
		URIType: "station", Resolver: true,
		Args: []kit.Arg{{Name: "city", Help: "city name, @uid, or geo:lat;lng"}},
	}, getFeed)

	kit.Handle(app, kit.OpMeta{
		Name: "search", Group: "read", List: true,
		Summary: "Search monitoring stations by keyword",
		Args:    []kit.Arg{{Name: "query", Help: "city or station name to search"}},
	}, searchStations)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := DefaultConfig()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.Timeout = cfg.Timeout
	}
	if t := os.Getenv("WAQI_TOKEN"); t != "" {
		c.Token = t
	}
	return NewClient(c), nil
}

// --- inputs ---

type feedInput struct {
	City   string  `kit:"arg" help:"city name, @uid, or geo:lat;lng"`
	Client *Client `kit:"inject"`
}

type searchInput struct {
	Query  string  `kit:"arg" help:"city or station name to search"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

// --- handlers ---

func getFeed(ctx context.Context, in feedInput, emit func(*Station) error) error {
	s, err := in.Client.Feed(ctx, in.City)
	if err != nil {
		return mapErr(err)
	}
	return emit(s)
}

func searchStations(ctx context.Context, in searchInput, emit func(*SearchResult) error) error {
	results, err := in.Client.Search(ctx, in.Query)
	if err != nil {
		return mapErr(err)
	}
	for i := range results {
		if in.Limit > 0 && i >= in.Limit {
			break
		}
		if err := emit(&results[i]); err != nil {
			return err
		}
	}
	return nil
}

// mapErr converts a library error into the kit error kind that carries the right
// exit code.
func mapErr(err error) error {
	return errs.NotFound("%s", err.Error())
}
