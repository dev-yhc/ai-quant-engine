// Package postgres reads normalized valuation inputs from the shared database.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yhc/quant-engine-go/apps/valuation-engine/internal/application"
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

// RecordUS10YearSignal persists the daily event and its outbox row in one
// transaction. A retry for the same UTC evaluation day returns the same event.
func (r *Repository) RecordUS10YearSignal(ctx context.Context, result domain.US10YearResult) (application.SignalRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return application.SignalRecord{}, fmt.Errorf("begin signal transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	const instrument = "US_TREASURY_10Y"
	const modelVersion = "valuation-anchor-v1"
	evaluationDay := time.Now().UTC().Format("2006-01-02")
	var previousSignal string
	err = tx.QueryRow(ctx, `SELECT signal FROM valuation.signal_state WHERE instrument = $1 FOR UPDATE`, instrument).Scan(&previousSignal)
	if err != nil && err != pgx.ErrNoRows {
		return application.SignalRecord{}, fmt.Errorf("load signal state: %w", err)
	}
	approvalRequired := result.Signal != "FAIR" && previousSignal != result.Signal

	_, err = tx.Exec(ctx, `
INSERT INTO valuation.signal_evaluations (instrument, evaluated_on, as_of, model_version, actual, anchor, raw_distance, delta, z_score, signal)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (instrument, evaluated_on, model_version) DO UPDATE SET
 as_of = EXCLUDED.as_of, actual = EXCLUDED.actual, anchor = EXCLUDED.anchor,
 raw_distance = EXCLUDED.raw_distance, delta = EXCLUDED.delta, z_score = EXCLUDED.z_score, signal = EXCLUDED.signal`,
		instrument, evaluationDay, result.Date.UTC().Format("2006-01-02"), modelVersion, result.Actual, result.Anchor, result.RawDistance, result.Delta, result.ZScore, result.Signal)
	if err != nil {
		return application.SignalRecord{}, fmt.Errorf("upsert signal evaluation: %w", err)
	}

	eventKey := fmt.Sprintf("%s:%s:%s", instrument, evaluationDay, modelVersion)
	var eventID int64
	err = tx.QueryRow(ctx, `
INSERT INTO valuation.signal_events (event_key, instrument, evaluated_on, as_of, model_version, actual, anchor, raw_distance, delta, z_score, signal, approval_required)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (event_key) DO UPDATE SET
 actual = EXCLUDED.actual, anchor = EXCLUDED.anchor, raw_distance = EXCLUDED.raw_distance,
 delta = EXCLUDED.delta, z_score = EXCLUDED.z_score, signal = EXCLUDED.signal
RETURNING id`, eventKey, instrument, evaluationDay, result.Date.UTC().Format("2006-01-02"), modelVersion, result.Actual, result.Anchor, result.RawDistance, result.Delta, result.ZScore, result.Signal, approvalRequired).Scan(&eventID)
	if err != nil {
		return application.SignalRecord{}, fmt.Errorf("upsert signal event: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO valuation.signal_state (instrument, signal, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT (instrument) DO UPDATE SET signal = EXCLUDED.signal, updated_at = EXCLUDED.updated_at`, instrument, result.Signal)
	if err != nil {
		return application.SignalRecord{}, fmt.Errorf("upsert signal state: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"event_id": eventID, "event_key": eventKey, "instrument": instrument, "evaluated_on": evaluationDay,
		"as_of": result.Date.UTC().Format("2006-01-02"), "model_version": modelVersion, "actual": result.Actual,
		"anchor": result.Anchor, "raw_distance": result.RawDistance, "delta": result.Delta, "z_score": result.ZScore,
		"signal": result.Signal, "approval_required": approvalRequired,
	})
	if err != nil {
		return application.SignalRecord{}, fmt.Errorf("marshal signal outbox payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO valuation.signal_outbox (signal_event_id, event_type, payload)
VALUES ($1, 'valuation.us10y.evaluated', $2)
ON CONFLICT (signal_event_id) DO NOTHING`, eventID, payload)
	if err != nil {
		return application.SignalRecord{}, fmt.Errorf("enqueue signal outbox: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return application.SignalRecord{}, fmt.Errorf("commit signal transaction: %w", err)
	}
	return application.SignalRecord{EventID: eventID, EventKey: eventKey, ApprovalRequired: approvalRequired}, nil
}
