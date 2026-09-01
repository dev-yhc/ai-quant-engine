-- Baseline shared market-data storage.
--
-- Keep this migration portable across PostgreSQL-compatible databases: do not
-- depend on Supabase Auth, Storage, RLS, or Supabase-specific extensions.
-- Future schema changes must be added as new, ordered migration files.

CREATE SCHEMA IF NOT EXISTS market_data;

CREATE TABLE market_data.observations (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    provider VARCHAR(100) NOT NULL,
    series VARCHAR(255) NOT NULL,
    observed_at DATE NOT NULL,
    value DOUBLE PRECISION NOT NULL,
    collected_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (provider, series, observed_at)
);

CREATE INDEX observations_series_observed_at_idx
    ON market_data.observations (series, observed_at DESC);

CREATE TABLE market_data.research_datasets (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    provider VARCHAR(100) NOT NULL,
    name VARCHAR(255) NOT NULL,
    content_type VARCHAR(255) NOT NULL,
    content BYTEA NOT NULL,
    source_url TEXT,
    fetched_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (provider, name, fetched_at)
);

CREATE INDEX research_datasets_provider_name_fetched_at_idx
    ON market_data.research_datasets (provider, name, fetched_at DESC);
