// Package fred implements the TimeSeriesProvider port using the FRED API.
package fred

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/yhc/quant-engine-go/domains/marketdata/domain"
)

const defaultObservationsURL = "https://api.stlouisfed.org/fred/series/observations"

type Adapter struct {
	apiKey          string
	observationsURL string
	httpClient      *http.Client
}

func New(apiKey string, client *http.Client) (*Adapter, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("FRED_API_KEY is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Adapter{apiKey: apiKey, observationsURL: defaultObservationsURL, httpClient: client}, nil
}

// WithObservationsURL is primarily useful for integration tests or a proxy.
func (a *Adapter) WithObservationsURL(rawURL string) *Adapter {
	a.observationsURL = rawURL
	return a
}

type observationsResponse struct {
	Observations []struct {
		Date  string `json:"date"`
		Value string `json:"value"`
	} `json:"observations"`
}

func (a *Adapter) Observations(ctx context.Context, seriesIDs []string) ([]domain.Observation, error) {
	var results []domain.Observation
	for _, seriesID := range seriesIDs {
		observations, err := a.seriesObservations(ctx, seriesID)
		if err != nil {
			return nil, err
		}
		results = append(results, observations...)
	}
	return results, nil
}

func (a *Adapter) seriesObservations(ctx context.Context, seriesID string) ([]domain.Observation, error) {
	endpoint, err := url.Parse(a.observationsURL)
	if err != nil {
		return nil, fmt.Errorf("parse FRED URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("series_id", seriesID)
	query.Set("api_key", a.apiKey)
	query.Set("file_type", "json")
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create FRED request: %w", err)
	}
	response, err := a.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request FRED series %s: %w", seriesID, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("FRED series %s returned HTTP %d", seriesID, response.StatusCode)
	}

	var payload observationsResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode FRED series %s: %w", seriesID, err)
	}
	results := make([]domain.Observation, 0, len(payload.Observations))
	for _, item := range payload.Observations {
		if item.Value == "." || item.Value == "" { // FRED missing observation marker.
			continue
		}
		observedAt, err := time.Parse("2006-01-02", item.Date)
		if err != nil {
			return nil, fmt.Errorf("parse FRED date %q for %s: %w", item.Date, seriesID, err)
		}
		value, err := strconv.ParseFloat(item.Value, 64)
		if err != nil {
			return nil, fmt.Errorf("parse FRED value %q for %s: %w", item.Value, seriesID, err)
		}
		results = append(results, domain.Observation{Series: seriesID, ObservedAt: observedAt, Value: value, Provider: "fred"})
	}
	return results, nil
}
