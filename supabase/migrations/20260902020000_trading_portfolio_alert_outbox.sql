-- trading-engine owns portfolio snapshots. alert-dispatcher consumes this
-- outbox only; it never reads the trading book or account credentials.
CREATE TABLE trading.portfolio_alert_outbox (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING',
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX portfolio_alert_outbox_pending_idx
    ON trading.portfolio_alert_outbox (status, available_at, id);
