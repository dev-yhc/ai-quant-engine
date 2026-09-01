// Package application contains collector use cases (activities).
package application

import (
	"context"
	"fmt"
	"time"
)

const (
	CollectFredValuationActivityName  = "collect-fred-valuation-data"
	CollectNYFedValuationActivityName = "collect-ny-fed-valuation-data"
)

var FredValuationSeries = []string{"DGS10", "T10YIE", "DFII10", "DGS2", "DGS3MO", "CPIAUCSL", "A191RL1Q225SBEA"}
var NYFedValuationDatasets = []string{"acm_term_premium", "hlw_r_star"}

type FredCollectionResult struct {
	Provider          string
	Series            []string
	ObservationCount  int
	LatestObservation time.Time
}

// Activities supplies the adapters required by Temporal activity handlers.
// Workflow code remains deterministic and never receives these dependencies.
type Activities struct {
	FredProvider  TimeSeriesProvider
	NYFedProvider ResearchFileProvider
	Repository    MarketDataRepository
}

func (a Activities) CollectFredValuationData(ctx context.Context) (FredCollectionResult, error) {
	return CollectFredValuationData(ctx, a.FredProvider, a.Repository)
}

func (a Activities) CollectNYFedValuationData(ctx context.Context) (NYFedCollectionResult, error) {
	return CollectNYFedValuationData(ctx, a.NYFedProvider, a.Repository)
}

// CollectFredValuationData is one independently retryable activity.
func CollectFredValuationData(ctx context.Context, provider TimeSeriesProvider, repository MarketDataRepository) (FredCollectionResult, error) {
	observations, err := provider.Observations(ctx, FredValuationSeries)
	if err != nil {
		return FredCollectionResult{}, fmt.Errorf("collect FRED valuation data: %w", err)
	}
	if err := repository.SaveObservations(ctx, observations); err != nil {
		return FredCollectionResult{}, fmt.Errorf("persist FRED valuation data: %w", err)
	}

	result := FredCollectionResult{Provider: "fred", Series: FredValuationSeries, ObservationCount: len(observations)}
	for _, observation := range observations {
		if observation.ObservedAt.After(result.LatestObservation) {
			result.LatestObservation = observation.ObservedAt
		}
	}
	return result, nil
}

type NYFedDatasetResult struct {
	Name        string
	Bytes       int
	ContentType string
}

type NYFedCollectionResult struct {
	Provider string
	Datasets []NYFedDatasetResult
}

// CollectNYFedValuationData is a separate activity so provider failures and
// retry policies remain isolated from FRED collection.
func CollectNYFedValuationData(ctx context.Context, provider ResearchFileProvider, repository MarketDataRepository) (NYFedCollectionResult, error) {
	result := NYFedCollectionResult{Provider: "ny_fed"}
	for _, name := range NYFedValuationDatasets {
		dataset, err := provider.Dataset(ctx, name)
		if err != nil {
			return NYFedCollectionResult{}, fmt.Errorf("collect NY Fed dataset %q: %w", name, err)
		}
		if err := repository.SaveResearchDataset(ctx, dataset); err != nil {
			return NYFedCollectionResult{}, fmt.Errorf("persist NY Fed dataset %q: %w", name, err)
		}
		result.Datasets = append(result.Datasets, NYFedDatasetResult{
			Name: dataset.Name, Bytes: len(dataset.Content), ContentType: dataset.ContentType,
		})
	}
	return result, nil
}
