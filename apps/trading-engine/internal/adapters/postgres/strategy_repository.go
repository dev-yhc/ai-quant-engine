package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	appstrategy "github.com/yhc/quant-engine-go/apps/trading-engine/internal/application/strategy"
	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain"
	strategydomain "github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain/strategy"
)

func (r *Repository) LoadPolicy(ctx context.Context, strategyID string) (strategydomain.LadderPolicy, error) {
	var policy strategydomain.LadderPolicy
	var bandsJSON []byte
	err := r.pool.QueryRow(ctx, `SELECT strategy_id, instrument, policy_version, enabled, direction_multiplier, entry_bands, max_exposure_krw::text, max_portfolio_weight::text, max_order_step_krw::text, require_overvalued_signal
FROM trading.strategy_configs WHERE strategy_id=$1`, strategyID).Scan(
		&policy.StrategyID, &policy.Instrument, &policy.PolicyVersion, &policy.Enabled, &policy.DirectionMultiplier, &bandsJSON, &policy.MaxExposureKRW, &policy.MaxPortfolioWeight, &policy.MaxOrderStepKRW, &policy.RequireOvervaluedSignal)
	if err != nil {
		return strategydomain.LadderPolicy{}, err
	}
	var stored []struct {
		ScoreThreshold float64 `json:"score_threshold"`
		TargetKRW      string  `json:"target_krw"`
	}
	if err := json.Unmarshal(bandsJSON, &stored); err != nil {
		return strategydomain.LadderPolicy{}, fmt.Errorf("decode entry bands: %w", err)
	}
	policy.EntryBands = make([]strategydomain.Band, len(stored))
	for i, band := range stored {
		policy.EntryBands[i] = strategydomain.Band{ScoreThreshold: band.ScoreThreshold, TargetKRW: band.TargetKRW}
	}
	if err := policy.Validate(); err != nil {
		return strategydomain.LadderPolicy{}, err
	}
	return policy, nil
}

func (r *Repository) FindDecision(ctx context.Context, signalEventID, strategyID string) (appstrategy.Decision, bool, error) {
	row := r.pool.QueryRow(ctx, `SELECT id, signal_event_id, strategy_id, instrument, model_version, policy_version, z_score, signal, as_of::text, target_krw::text, target_weight::text, effective_exposure_krw::text, delta_krw::text, order_amount_krw::text, COALESCE(order_id,''), reason
FROM trading.strategy_decisions WHERE signal_event_id=$1 AND strategy_id=$2`, signalEventID, strategyID)
	return scanDecision(row)
}

// PendingExposure counts orders that may still change the account exposure.
// UNKNOWN remains included conservatively until an account reconciliation
// resolves it.
func (r *Repository) PendingExposure(ctx context.Context, strategyID, instrument string) (strategydomain.Exposure, error) {
	var exposure strategydomain.Exposure
	err := r.pool.QueryRow(ctx, `SELECT
COALESCE(SUM(CASE WHEN o.side='BUY' THEN d.order_amount_krw ELSE 0 END), 0)::text,
COALESCE(SUM(CASE WHEN o.side='SELL' THEN d.order_amount_krw ELSE 0 END), 0)::text
FROM trading.strategy_decisions d
JOIN trading.orders o ON o.id=d.order_id
WHERE d.strategy_id=$1 AND d.instrument=$2
  AND o.status IN ('PENDING','PROCESSING','SUBMITTED','UNKNOWN')`, strategyID, instrument).Scan(&exposure.PendingBuyKRW, &exposure.PendingSellKRW)
	return exposure, err
}

// SaveDecisionAndOrder atomically records a strategy decision and, where
// applicable, inserts the corresponding existing order lifecycle record.
func (r *Repository) SaveDecisionAndOrder(ctx context.Context, decision appstrategy.Decision, intent *domain.Intent, now time.Time) (appstrategy.Decision, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return appstrategy.Decision{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialise the unique decision key before inserting an order so a relay
	// retry cannot insert an orphan duplicate order in a concurrent transaction.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, decision.StrategyID+"\x00"+decision.SignalEventID); err != nil {
		return appstrategy.Decision{}, false, err
	}
	if existing, found, err := findDecision(ctx, tx, decision.SignalEventID, decision.StrategyID); err != nil {
		return appstrategy.Decision{}, false, err
	} else if found {
		return existing, false, tx.Commit(ctx)
	}
	if intent != nil {
		if _, err := tx.Exec(ctx, `INSERT INTO trading.orders (id, signal_event_id, strategy, instrument, side, order_type, quantity, order_amount, limit_price, idempotency_key, policy_version, execution_mode, expires_at, status, broker_client_order_id, next_attempt_at, broker_idempotency_until)
VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),$10,$11,$12,$13,'PENDING',$14,$15,$16)`,
			intent.ID, intent.SignalEventID, intent.Strategy, intent.Instrument, intent.Side, intent.OrderType, intent.Quantity, intent.OrderAmount, intent.LimitPrice, intent.IdempotencyKey, intent.PolicyVersion, intent.Mode, intent.ExpiresAt, intent.TossClientOrderID(), now, now.Add(9*time.Minute)); err != nil {
			return appstrategy.Decision{}, false, fmt.Errorf("insert strategy order: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO trading.strategy_decisions (id, signal_event_id, strategy_id, instrument, model_version, policy_version, z_score, signal, as_of, target_krw, target_weight, effective_exposure_krw, delta_krw, order_amount_krw, order_id, reason)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,NULLIF($15,''),$16)`,
		decision.ID, decision.SignalEventID, decision.StrategyID, decision.Instrument, decision.ModelVersion, decision.PolicyVersion, decision.ZScore, decision.Signal, decision.AsOf, decimalOrZero(decision.TargetKRW), decimalOrZero(decision.TargetWeight), decimalOrZero(decision.EffectiveExposureKRW), decimalOrZero(decision.DeltaKRW), decimalOrZero(decision.OrderAmountKRW), decision.OrderID, decision.Reason); err != nil {
		return appstrategy.Decision{}, false, fmt.Errorf("insert strategy decision: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return appstrategy.Decision{}, false, err
	}
	return decision, true, nil
}

type decisionScanner interface{ Scan(...any) error }

func findDecision(ctx context.Context, tx pgx.Tx, signalEventID, strategyID string) (appstrategy.Decision, bool, error) {
	row := tx.QueryRow(ctx, `SELECT id, signal_event_id, strategy_id, instrument, model_version, policy_version, z_score, signal, as_of::text, target_krw::text, target_weight::text, effective_exposure_krw::text, delta_krw::text, order_amount_krw::text, COALESCE(order_id,''), reason
FROM trading.strategy_decisions WHERE signal_event_id=$1 AND strategy_id=$2`, signalEventID, strategyID)
	return scanDecision(row)
}

func scanDecision(row decisionScanner) (appstrategy.Decision, bool, error) {
	var decision appstrategy.Decision
	err := row.Scan(&decision.ID, &decision.SignalEventID, &decision.StrategyID, &decision.Instrument, &decision.ModelVersion, &decision.PolicyVersion, &decision.ZScore, &decision.Signal, &decision.AsOf, &decision.TargetKRW, &decision.TargetWeight, &decision.EffectiveExposureKRW, &decision.DeltaKRW, &decision.OrderAmountKRW, &decision.OrderID, &decision.Reason)
	if err == pgx.ErrNoRows {
		return appstrategy.Decision{}, false, nil
	}
	return decision, err == nil, err
}

func decimalOrZero(value string) string {
	if value == "" {
		return "0"
	}
	return value
}
