// Package postgres reads normalized valuation inputs from the shared database.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yhc/quant-engine-go/domains/valuation/domain"
)

const (
	seriesActualYield = "DGS10"
	seriesBreakeven   = "T10YIE"
	seriesRealYield   = "DFII10"
	seriesTermPremium = "ACM_TERM_PREMIUM"
	seriesNaturalRate = "HLW_R_STAR"
	seriesTreasury2Y  = "DGS2"
	seriesTreasury3M  = "DGS3MO"
	seriesCPI         = "CPIAUCSL"
	seriesGDPGrowth   = "A191RL1Q225SBEA"
)

var requiredSeries = []string{seriesActualYield, seriesBreakeven, seriesRealYield, seriesTermPremium, seriesNaturalRate, seriesTreasury2Y, seriesTreasury3M, seriesCPI, seriesGDPGrowth}

type Repository struct{ pool *pgxpool.Pool }

func New(ctx context.Context, connectionURL string) (*Repository, error) {
	if connectionURL == "" {
		return nil, fmt.Errorf("DATABASE_CONNECTION_URL is required")
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

func (r *Repository) LoadUS10YearInput(ctx context.Context) (domain.US10YearInput, error) {
	rows, err := r.pool.Query(ctx, `
SELECT series, observed_at, value
FROM (
    SELECT DISTINCT ON (series, observed_at) series, observed_at, value
    FROM market_data.observations
    WHERE series = ANY($1)
    ORDER BY series, observed_at, collected_at DESC
) latest
ORDER BY observed_at`, requiredSeries)
	if err != nil {
		return domain.US10YearInput{}, fmt.Errorf("query valuation observations: %w", err)
	}
	defer rows.Close()

	values := make(map[string][]domain.Observation, len(requiredSeries))
	for rows.Next() {
		var series string
		var date time.Time
		var value float64
		if err := rows.Scan(&series, &date, &value); err != nil {
			return domain.US10YearInput{}, fmt.Errorf("scan valuation observation: %w", err)
		}
		values[series] = append(values[series], domain.Observation{Date: date, Value: value})
	}
	if err := rows.Err(); err != nil {
		return domain.US10YearInput{}, fmt.Errorf("read valuation observations: %w", err)
	}
	return domain.US10YearInput{
		ActualYield: values[seriesActualYield], BreakevenInflation: values[seriesBreakeven], RealYield: values[seriesRealYield],
		TermPremium: values[seriesTermPremium], NaturalRate: values[seriesNaturalRate], Treasury2Year: values[seriesTreasury2Y],
		Treasury3Month: values[seriesTreasury3M], CPI: values[seriesCPI], GDPGrowth: values[seriesGDPGrowth],
	}, nil
}
