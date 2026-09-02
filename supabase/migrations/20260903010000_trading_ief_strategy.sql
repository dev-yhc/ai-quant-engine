-- ADR-003: strategy planning is stored separately from the existing order
-- lifecycle. Orders remain owned by trading.orders and its worker.
CREATE TABLE trading.strategy_configs (
    strategy_id TEXT PRIMARY KEY,
    instrument TEXT NOT NULL,
    policy_version TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    direction_multiplier NUMERIC NOT NULL,
    entry_bands JSONB NOT NULL,
    max_exposure_krw NUMERIC NOT NULL,
    max_portfolio_weight NUMERIC NOT NULL,
    max_order_step_krw NUMERIC NOT NULL,
    require_overvalued_signal BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (max_exposure_krw > 0),
    CHECK (max_portfolio_weight > 0 AND max_portfolio_weight <= 1),
    CHECK (max_order_step_krw > 0)
);

INSERT INTO trading.strategy_configs (
    strategy_id, instrument, policy_version, enabled, direction_multiplier,
    entry_bands, max_exposure_krw, max_portfolio_weight, max_order_step_krw,
    require_overvalued_signal
) VALUES (
    'us10y-overvalued-ief', 'US:IEF', 'v1', TRUE, -1,
    '[{"score_threshold":0.5,"target_krw":"500000"},{"score_threshold":1.0,"target_krw":"1000000"}]'::jsonb,
    1000000, 1, 500000, TRUE
);

CREATE TABLE trading.strategy_decisions (
    id TEXT PRIMARY KEY,
    signal_event_id TEXT NOT NULL,
    strategy_id TEXT NOT NULL REFERENCES trading.strategy_configs(strategy_id),
    instrument TEXT NOT NULL,
    model_version TEXT NOT NULL,
    policy_version TEXT NOT NULL,
    z_score DOUBLE PRECISION NOT NULL,
    signal TEXT NOT NULL,
    as_of DATE NOT NULL,
    target_krw NUMERIC NOT NULL,
    target_weight NUMERIC NOT NULL,
    effective_exposure_krw NUMERIC NOT NULL,
    delta_krw NUMERIC NOT NULL,
    order_amount_krw NUMERIC NOT NULL,
    order_id TEXT REFERENCES trading.orders(id),
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (signal_event_id, strategy_id)
);

CREATE INDEX strategy_decisions_open_order_idx
    ON trading.strategy_decisions (strategy_id, instrument, order_id)
    WHERE order_id IS NOT NULL;
