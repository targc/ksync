CREATE TABLE ksync_api_tokens (
    id         UUID PRIMARY KEY,
    token      TEXT NOT NULL UNIQUE,
    cluster    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
