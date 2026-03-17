package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client is a minimal OSRM HTTP client.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// RouteResult holds the routing response we care about.
type RouteResult struct {
	DistanceMeters  int
	DurationSeconds int
	Polyline        string // encoded polyline
}

// Route calls OSRM route API between two points.
// Falls back gracefully if OSRM is unavailable (returns haversine estimate).
func (c *Client) Route(ctx context.Context, fromLat, fromLng, toLat, toLng float64) (*RouteResult, error) {
	url := fmt.Sprintf("%s/route/v1/driving/%f,%f;%f,%f?overview=simplified&geometries=polyline",
		c.baseURL, fromLng, fromLat, toLng, toLat)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// OSRM not running — return nil so caller can use fallback
		return nil, fmt.Errorf("osrm unavailable: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Code   string `json:"code"`
		Routes []struct {
			Distance float64 `json:"distance"`
			Duration float64 `json:"duration"`
			Geometry string  `json:"geometry"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("osrm decode: %w", err)
	}
	if result.Code != "Ok" || len(result.Routes) == 0 {
		return nil, fmt.Errorf("osrm no route: code=%s", result.Code)
	}

	r := result.Routes[0]
	return &RouteResult{
		DistanceMeters:  int(r.Distance),
		DurationSeconds: int(r.Duration),
		Polyline:        r.Geometry,
	}, nil
}
