// Package postgres implements market-data persistence for PostgreSQL-compatible
// databases, including Supabase.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yhc/quant-engine-go/domains/marketdata/domain"
)

const schema = `
CREATE TABLE IF NOT EXISTS market_observations (
    provider TEXT NOT NULL,
    series TEXT NOT NULL,
    observed_at DATE NOT NULL,
    value DOUBLE PRECISION NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, series, observed_at)
);
CREATE INDEX IF NOT EXISTS market_observations_series_date_idx
    ON market_observations (series, observed_at DESC);
CREATE TABLE IF NOT EXISTS research_datasets (
    provider TEXT NOT NULL,
    name TEXT NOT NULL,
    content BYTEA NOT NULL,
    content_type TEXT NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, name)
);`

type Repository struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, connectionURL string) (*Repository, error) {
	if connectionURL == "" {
		return nil, fmt.Errorf("database connection URL is required")
	}
	pool, err := pgxpool.New(ctx, connectionURL)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	return &Repository{pool: pool}, nil
}

func (r *Repository) Close() { r.pool.Close() }

func (r *Repository) EnsureSchema(ctx context.Context) error {
	if _, err := r.pool.Exec(ctx, schema); err != nil {
		return fmt.Errorf("ensure market-data schema: %w", err)
	}
	return nil
}

func (r *Repository) SaveObservations(ctx context.Context, observations []domain.Observation) error {
	if len(observations) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, observation := range observations {
		batch.Queue(`
INSERT INTO market_observations (provider, series, observed_at, value, collected_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (provider, series, observed_at)
DO UPDATE SET value = EXCLUDED.value, collected_at = now()`,
			observation.Provider, observation.Series, observation.ObservedAt, observation.Value)
	}
	results := r.pool.SendBatch(ctx, batch)
	for range observations {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return fmt.Errorf("upsert market observations: %w", err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("close observation batch: %w", err)
	}
	return nil
}

func (r *Repository) SaveResearchDataset(ctx context.Context, dataset domain.ResearchDataset) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO research_datasets (provider, name, content, content_type, collected_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (provider, name)
DO UPDATE SET content = EXCLUDED.content, content_type = EXCLUDED.content_type, collected_at = now()`,
		dataset.Provider, dataset.Name, dataset.Content, dataset.ContentType)
	if err != nil {
		return fmt.Errorf("upsert research dataset: %w", err)
	}
	return nil
}

type Counts struct {
	Observations int64
	Datasets     int64
}

func (r *Repository) Counts(ctx context.Context) (Counts, error) {
	var counts Counts
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM market_observations").Scan(&counts.Observations); err != nil {
		return Counts{}, fmt.Errorf("count market observations: %w", err)
	}
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM research_datasets").Scan(&counts.Datasets); err != nil {
		return Counts{}, fmt.Errorf("count research datasets: %w", err)
	}
	return counts, nil
}
