// Package application contains collector use cases (activities).
package application

import (
	"context"
	"fmt"
	"time"
)

var FredValuationSeries = []string{"DGS10", "T10YIE", "DFII10", "DGS2", "DGS3MO"}
var NYFedValuationDatasets = []string{"acm_term_premium", "hlw_r_star"}

type FredCollectionResult struct {
	Provider          string
	Series            []string
	ObservationCount  int
	LatestObservation time.Time
}

// CollectFredValuationData is one independently retryable activity.
func CollectFredValuationData(ctx context.Context, provider TimeSeriesProvider) (FredCollectionResult, error) {
	observations, err := provider.Observations(ctx, FredValuationSeries)
	if err != nil {
		return FredCollectionResult{}, fmt.Errorf("collect FRED valuation data: %w", err)
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
func CollectNYFedValuationData(ctx context.Context, provider ResearchFileProvider) (NYFedCollectionResult, error) {
	result := NYFedCollectionResult{Provider: "ny_fed"}
	for _, name := range NYFedValuationDatasets {
		dataset, err := provider.Dataset(ctx, name)
		if err != nil {
			return NYFedCollectionResult{}, fmt.Errorf("collect NY Fed dataset %q: %w", name, err)
		}
		result.Datasets = append(result.Datasets, NYFedDatasetResult{
			Name: dataset.Name, Bytes: len(dataset.Content), ContentType: dataset.ContentType,
		})
	}
	return result, nil
}
