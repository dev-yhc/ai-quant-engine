// Package postgres persists the trading engine's independent order lifecycle.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yhc/quant-engine-go/apps/trading-engine/internal/domain"
)

type Repository struct{ pool *pgxpool.Pool }

func New(ctx context.Context, connectionURL string) (*Repository, error) {
	if connectionURL == "" {
		return nil, fmt.Errorf("TRADING_DATABASE_CONNECTION_URL is required")
	}
	pool, err := pgxpool.New(ctx, connectionURL)
	if err != nil {
		return nil, fmt.Errorf("create trading PostgreSQL pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to trading PostgreSQL: %w", err)
	}
	return &Repository{pool: pool}, nil
}
func (r *Repository) Close() { r.pool.Close() }

func (r *Repository) Accept(ctx context.Context, i domain.Intent, now time.Time) (domain.Order, bool, error) {
	row := r.pool.QueryRow(ctx, `INSERT INTO trading.orders (id, signal_event_id, approval_request_id, strategy, instrument, side, order_type, quantity, order_amount, limit_price, idempotency_key, policy_version, execution_mode, expires_at, status, broker_client_order_id, next_attempt_at, broker_idempotency_until)
VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),$11,$12,$13,$14,'PENDING',$15,$16,$17)
ON CONFLICT (idempotency_key) DO NOTHING RETURNING `+orderColumns,
		i.ID, i.SignalEventID, i.ApprovalRequestID, i.Strategy, i.Instrument, i.Side, i.OrderType, i.Quantity, i.OrderAmount, i.LimitPrice, i.IdempotencyKey, i.PolicyVersion, i.Mode, i.ExpiresAt, i.TossClientOrderID(), now, now.Add(9*time.Minute))
	o, err := scanOrder(row)
	if err == nil {
		return o, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, false, err
	}
	o, err = r.findByKey(ctx, i.IdempotencyKey)
	return o, false, err
}
func (r *Repository) findByKey(ctx context.Context, key string) (domain.Order, error) {
	return scanOrder(r.pool.QueryRow(ctx, `SELECT `+orderColumns+` FROM trading.orders WHERE idempotency_key=$1`, key))
}
func (r *Repository) ClaimNext(ctx context.Context, now time.Time) (domain.Order, bool, error) {
	row := r.pool.QueryRow(ctx, `WITH candidate AS (SELECT id FROM trading.orders WHERE status='PENDING' AND next_attempt_at <= $1 ORDER BY next_attempt_at FOR UPDATE SKIP LOCKED LIMIT 1)
UPDATE trading.orders o SET status='PROCESSING', updated_at=$1 FROM candidate WHERE o.id=candidate.id RETURNING `+orderColumns, now)
	o, err := scanOrder(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, false, nil
	}
	return o, err == nil, err
}
func (r *Repository) MarkSubmitted(ctx context.Context, id, brokerID string, now time.Time) error {
	tag, err := r.pool.Exec(ctx, `UPDATE trading.orders SET status='SUBMITTED', broker_order_id=$2, submitted_at=$3, updated_at=$3, last_error=NULL WHERE id=$1 AND status='PROCESSING'`, id, brokerID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("order %s was not processing", id)
	}
	return nil
}
func (r *Repository) Reschedule(ctx context.Context, id string, attempt int, next time.Time, message string) error {
	return r.transition(ctx, id, domain.Pending, attempt, next, message)
}
func (r *Repository) MarkRejected(ctx context.Context, id, message string, now time.Time) error {
	return r.transition(ctx, id, domain.Rejected, 0, now, message)
}
func (r *Repository) MarkUnknown(ctx context.Context, id, message string, now time.Time) error {
	return r.transition(ctx, id, domain.Unknown, 0, now, message)
}
func (r *Repository) transition(ctx context.Context, id string, status domain.Status, attempt int, next time.Time, message string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE trading.orders SET status=$2, attempt_count=CASE WHEN $3=0 THEN attempt_count ELSE $3 END, next_attempt_at=$4, last_error=$5, updated_at=NOW() WHERE id=$1 AND status='PROCESSING'`, id, status, attempt, next, message)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("order %s was not processing", id)
	}
	return nil
}

const orderColumns = `id,signal_event_id,COALESCE(approval_request_id,''),strategy,instrument,side,order_type,COALESCE(quantity::text,''),COALESCE(order_amount::text,''),COALESCE(limit_price::text,''),idempotency_key,policy_version,execution_mode,expires_at,status,COALESCE(broker_order_id,''),broker_client_order_id,attempt_count,next_attempt_at,broker_idempotency_until,COALESCE(last_error,'')`

type rowScanner interface{ Scan(...any) error }

func scanOrder(row rowScanner) (domain.Order, error) {
	var o domain.Order
	err := row.Scan(&o.ID, &o.SignalEventID, &o.ApprovalRequestID, &o.Strategy, &o.Instrument, &o.Side, &o.OrderType, &o.Quantity, &o.OrderAmount, &o.LimitPrice, &o.IdempotencyKey, &o.PolicyVersion, &o.Mode, &o.ExpiresAt, &o.Status, &o.BrokerOrderID, &o.BrokerClientOrderID, &o.AttemptCount, &o.NextAttemptAt, &o.BrokerIdempotencyTill, &o.LastError)
	return o, err
}
