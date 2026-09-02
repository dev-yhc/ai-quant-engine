package application

import (
	"context"

	"github.com/yhc/quant-engine-go/domains/marketdata/domain"
)

// TimeSeriesProvider is an outbound port. FRED is one implementation; a
// replacement vendor only needs to implement this contract.
type TimeSeriesProvider interface {
	Observations(ctx context.Context, seriesIDs []string) ([]domain.Observation, error)
}

// ResearchTimeSeriesProvider downloads a versioned research dataset and
// normalizes its valuation series before the activity persists both forms.
type ResearchTimeSeriesProvider interface {
	Dataset(ctx context.Context, name string) (domain.ResearchDataset, error)
	Observations(dataset domain.ResearchDataset) ([]domain.Observation, error)
}

// MarketDataRepository is the outbound persistence port shared by collection
// activities. PostgreSQL/Supabase is an adapter detail.
type MarketDataRepository interface {
	SaveObservations(ctx context.Context, observations []domain.Observation) error
	SaveResearchDataset(ctx context.Context, dataset domain.ResearchDataset) error
}
