-- Durable daily valuation events and alert routing state. This deliberately
-- uses PostgreSQL tables rather than an external broker at current volume.
CREATE SCHEMA IF NOT EXISTS valuation;

CREATE TABLE valuation.signal_evaluations (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    instrument TEXT NOT NULL,
    evaluated_on DATE NOT NULL,
    as_of DATE NOT NULL,
    model_version TEXT NOT NULL,
    actual DOUBLE PRECISION NOT NULL,
    anchor DOUBLE PRECISION NOT NULL,
    raw_distance DOUBLE PRECISION NOT NULL,
    delta DOUBLE PRECISION NOT NULL,
    z_score DOUBLE PRECISION NOT NULL,
    signal TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (instrument, evaluated_on, model_version)
);

CREATE TABLE valuation.signal_state (
    instrument TEXT PRIMARY KEY,
    signal TEXT NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE valuation.signal_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_key TEXT NOT NULL UNIQUE,
    instrument TEXT NOT NULL,
    evaluated_on DATE NOT NULL,
    as_of DATE NOT NULL,
    model_version TEXT NOT NULL,
    actual DOUBLE PRECISION NOT NULL,
    anchor DOUBLE PRECISION NOT NULL,
    raw_distance DOUBLE PRECISION NOT NULL,
    delta DOUBLE PRECISION NOT NULL,
    z_score DOUBLE PRECISION NOT NULL,
    signal TEXT NOT NULL,
    approval_required BOOLEAN NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE valuation.signal_outbox (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    signal_event_id BIGINT NOT NULL UNIQUE REFERENCES valuation.signal_events(id),
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING',
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    locked_at TIMESTAMP WITH TIME ZONE,
    processed_at TIMESTAMP WITH TIME ZONE,
    last_error TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX signal_outbox_pending_idx ON valuation.signal_outbox (status, available_at, id);

CREATE TABLE valuation.approval_requests (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    signal_event_id BIGINT NOT NULL UNIQUE REFERENCES valuation.signal_events(id),
    status TEXT NOT NULL DEFAULT 'PENDING',
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    decided_at TIMESTAMP WITH TIME ZONE,
    decided_by TEXT
);

CREATE TABLE valuation.alert_outbox (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    signal_event_id BIGINT NOT NULL REFERENCES valuation.signal_events(id),
    approval_request_id BIGINT REFERENCES valuation.approval_requests(id),
    alert_kind TEXT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING',
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    locked_at TIMESTAMP WITH TIME ZONE,
    delivered_at TIMESTAMP WITH TIME ZONE,
    last_error TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (signal_event_id, alert_kind)
);

CREATE INDEX alert_outbox_pending_idx ON valuation.alert_outbox (status, available_at, id);
