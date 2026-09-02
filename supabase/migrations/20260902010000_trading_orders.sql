-- trading-engine owns this schema. signal-dispatcher communicates only through gRPC.
CREATE SCHEMA IF NOT EXISTS trading;

CREATE TABLE trading.orders (
    id TEXT PRIMARY KEY,
    signal_event_id TEXT NOT NULL,
    approval_request_id TEXT,
    strategy TEXT NOT NULL,
    instrument TEXT NOT NULL,
    side TEXT NOT NULL CHECK (side IN ('BUY', 'SELL')),
    order_type TEXT NOT NULL CHECK (order_type IN ('MARKET', 'LIMIT')),
    quantity NUMERIC,
    order_amount NUMERIC,
    limit_price NUMERIC,
    idempotency_key TEXT NOT NULL UNIQUE,
    policy_version TEXT NOT NULL,
    execution_mode TEXT NOT NULL CHECK (execution_mode IN ('APPROVED', 'AUTO_SIGNAL')),
    expires_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSING', 'SUBMITTED', 'REJECTED', 'UNKNOWN')),
    broker_order_id TEXT,
    broker_client_order_id TEXT NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    broker_idempotency_until TIMESTAMPTZ NOT NULL,
    last_error TEXT,
    submitted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((quantity IS NULL) <> (order_amount IS NULL)),
    CHECK ((order_type = 'LIMIT' AND limit_price IS NOT NULL) OR (order_type = 'MARKET' AND limit_price IS NULL))
);
CREATE INDEX orders_pending_delivery_idx ON trading.orders (next_attempt_at) WHERE status = 'PENDING';
