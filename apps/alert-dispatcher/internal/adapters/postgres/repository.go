// Package postgres implements the PostgreSQL-backed signal and alert outboxes.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yhc/quant-engine-go/apps/alert-dispatcher/internal/application"
	tradingdomain "github.com/yhc/quant-engine-go/domains/trading/domain"
)

type Repository struct {
	valuationPool *pgxpool.Pool
	tradingPool   *pgxpool.Pool
}

func New(ctx context.Context, valuationConnectionURL, tradingConnectionURL string) (*Repository, error) {
	if valuationConnectionURL == "" {
		return nil, fmt.Errorf("DATABASE_CONNECTION_URL is required")
	}
	if tradingConnectionURL == "" {
		return nil, fmt.Errorf("TRADING_DATABASE_CONNECTION_URL is required")
	}
	valuationPool, err := pgxpool.New(ctx, valuationConnectionURL)
	if err != nil {
		return nil, fmt.Errorf("create valuation PostgreSQL pool: %w", err)
	}
	if err := valuationPool.Ping(ctx); err != nil {
		valuationPool.Close()
		return nil, fmt.Errorf("connect to valuation PostgreSQL: %w", err)
	}
	tradingPool, err := pgxpool.New(ctx, tradingConnectionURL)
	if err != nil {
		valuationPool.Close()
		return nil, fmt.Errorf("create trading PostgreSQL pool: %w", err)
	}
	if err := tradingPool.Ping(ctx); err != nil {
		valuationPool.Close()
		tradingPool.Close()
		return nil, fmt.Errorf("connect to trading PostgreSQL: %w", err)
	}
	return &Repository{valuationPool: valuationPool, tradingPool: tradingPool}, nil
}

func (r *Repository) Close() {
	r.valuationPool.Close()
	r.tradingPool.Close()
}

func (r *Repository) ClaimSignal(ctx context.Context) (application.SignalOutboxItem, bool, error) {
	row := r.valuationPool.QueryRow(ctx, `
WITH candidate AS (
 SELECT id FROM valuation.signal_outbox
 WHERE (status = 'PENDING' AND available_at <= NOW())
    OR (status = 'PROCESSING' AND locked_at < NOW() - INTERVAL '5 minutes')
 ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1
)
UPDATE valuation.signal_outbox outbox SET status = 'PROCESSING', attempts = outbox.attempts + 1, locked_at = NOW()
FROM candidate, valuation.signal_events event
WHERE outbox.id = candidate.id AND event.id = outbox.signal_event_id
RETURNING outbox.id, event.id, event.instrument, event.evaluated_on::text, event.as_of::text, event.model_version,
 event.actual, event.anchor, event.raw_distance, event.delta, event.z_score, event.signal, event.approval_required`)
	var item application.SignalOutboxItem
	err := row.Scan(&item.ID, &item.Event.ID, &item.Event.Instrument, &item.Event.EvaluatedOn, &item.Event.AsOf, &item.Event.ModelVersion,
		&item.Event.Actual, &item.Event.Anchor, &item.Event.RawDistance, &item.Event.Delta, &item.Event.ZScore, &item.Event.Signal, &item.Event.ApprovalRequired)
	if err == pgx.ErrNoRows {
		return application.SignalOutboxItem{}, false, nil
	}
	if err != nil {
		return application.SignalOutboxItem{}, false, err
	}
	return item, true, nil
}

func (r *Repository) RouteSignal(ctx context.Context, item application.SignalOutboxItem) error {
	tx, err := r.valuationPool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	infoPayload, err := json.Marshal(item.Event)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `
INSERT INTO valuation.alert_outbox (signal_event_id, alert_kind, payload)
VALUES ($1, 'INFORMATION', $2)
ON CONFLICT (signal_event_id, alert_kind) DO NOTHING`, item.Event.ID, infoPayload); err != nil {
		return err
	}
	if item.Event.ApprovalRequired {
		expiresAt := time.Now().UTC().Add(12 * time.Hour)
		var approvalID int64
		err = tx.QueryRow(ctx, `
INSERT INTO valuation.approval_requests (signal_event_id, expires_at)
VALUES ($1, $2)
ON CONFLICT (signal_event_id) DO UPDATE SET expires_at = valuation.approval_requests.expires_at
RETURNING id`, item.Event.ID, expiresAt).Scan(&approvalID)
		if err != nil {
			return err
		}
		approvalPayload, err := json.Marshal(struct {
			Event             application.SignalEvent `json:"event"`
			ApprovalRequestID int64                   `json:"approval_request_id"`
			ExpiresAt         time.Time               `json:"expires_at"`
		}{item.Event, approvalID, expiresAt})
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `
INSERT INTO valuation.alert_outbox (signal_event_id, approval_request_id, alert_kind, payload)
VALUES ($1, $2, 'APPROVAL_REQUEST', $3)
ON CONFLICT (signal_event_id, alert_kind) DO NOTHING`, item.Event.ID, approvalID, approvalPayload); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *Repository) MarkSignalDelivered(ctx context.Context, id int64) error {
	_, err := r.valuationPool.Exec(ctx, `UPDATE valuation.signal_outbox SET status = 'DELIVERED', processed_at = NOW(), locked_at = NULL WHERE id = $1`, id)
	return err
}

func (r *Repository) MarkSignalRetry(ctx context.Context, id int64, cause error) error {
	_, err := r.valuationPool.Exec(ctx, `UPDATE valuation.signal_outbox SET status = 'PENDING', available_at = NOW() + INTERVAL '1 minute', locked_at = NULL, last_error = $2 WHERE id = $1`, id, cause.Error())
	return err
}

func (r *Repository) ClaimAlert(ctx context.Context) (application.Alert, bool, error) {
	alert, found, err := r.claimValuationAlert(ctx)
	if err != nil || found {
		return alert, found, err
	}
	return r.claimPortfolioAlert(ctx)
}

func (r *Repository) claimValuationAlert(ctx context.Context) (application.Alert, bool, error) {
	row := r.valuationPool.QueryRow(ctx, `
WITH candidate AS (
 SELECT id FROM valuation.alert_outbox
 WHERE (status = 'PENDING' AND available_at <= NOW())
    OR (status = 'PROCESSING' AND locked_at < NOW() - INTERVAL '5 minutes')
 ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1
)
UPDATE valuation.alert_outbox outbox SET status = 'PROCESSING', attempts = outbox.attempts + 1, locked_at = NOW()
FROM candidate
WHERE outbox.id = candidate.id
RETURNING outbox.id, outbox.alert_kind, COALESCE(outbox.approval_request_id, 0), outbox.payload`)
	alert := application.Alert{Source: application.AlertSourceValuation}
	var payload []byte
	err := row.Scan(&alert.ID, &alert.Kind, &alert.ApprovalRequestID, &payload)
	if err == pgx.ErrNoRows {
		return application.Alert{}, false, nil
	}
	if err != nil {
		return application.Alert{}, false, err
	}
	if alert.Kind == application.AlertKindApproval {
		var decoded struct {
			Event             application.SignalEvent `json:"event"`
			ApprovalRequestID int64                   `json:"approval_request_id"`
			ExpiresAt         time.Time               `json:"expires_at"`
		}
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return application.Alert{}, false, err
		}
		alert.Event, alert.ApprovalRequestID, alert.ExpiresAt = decoded.Event, decoded.ApprovalRequestID, decoded.ExpiresAt
	} else if err := json.Unmarshal(payload, &alert.Event); err != nil {
		return application.Alert{}, false, err
	}
	return alert, true, nil
}

func (r *Repository) claimPortfolioAlert(ctx context.Context) (application.Alert, bool, error) {
	row := r.tradingPool.QueryRow(ctx, `
WITH candidate AS (
 SELECT id FROM trading.portfolio_alert_outbox
 WHERE (status = 'PENDING' AND available_at <= NOW())
    OR (status = 'PROCESSING' AND locked_at < NOW() - INTERVAL '5 minutes')
 ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1
)
UPDATE trading.portfolio_alert_outbox outbox SET status = 'PROCESSING', attempts = outbox.attempts + 1, locked_at = NOW()
FROM candidate
WHERE outbox.id = candidate.id
RETURNING outbox.id, outbox.payload`)
	alert := application.Alert{Source: application.AlertSourceTrading, Kind: application.AlertKindPortfolio}
	var payload []byte
	err := row.Scan(&alert.ID, &payload)
	if err == pgx.ErrNoRows {
		return application.Alert{}, false, nil
	}
	if err != nil {
		return application.Alert{}, false, err
	}
	portfolio := new(tradingdomain.Portfolio)
	if err := json.Unmarshal(payload, portfolio); err != nil {
		return application.Alert{}, false, err
	}
	alert.Portfolio = portfolio
	return alert, true, nil
}

func (r *Repository) MarkAlertDelivered(ctx context.Context, alert application.Alert) error {
	pool, table := r.valuationPool, "valuation.alert_outbox"
	if alert.Source == application.AlertSourceTrading {
		pool, table = r.tradingPool, "trading.portfolio_alert_outbox"
	}
	_, err := pool.Exec(ctx, `UPDATE `+table+` SET status = 'DELIVERED', delivered_at = NOW(), locked_at = NULL WHERE id = $1`, alert.ID)
	return err
}

func (r *Repository) MarkAlertRetry(ctx context.Context, alert application.Alert, cause error) error {
	pool, table := r.valuationPool, "valuation.alert_outbox"
	if alert.Source == application.AlertSourceTrading {
		pool, table = r.tradingPool, "trading.portfolio_alert_outbox"
	}
	_, err := pool.Exec(ctx, `UPDATE `+table+` SET status = 'PENDING', available_at = NOW() + INTERVAL '1 minute', locked_at = NULL, last_error = $2 WHERE id = $1`, alert.ID, cause.Error())
	return err
}
