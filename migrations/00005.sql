CREATE TABLE ksync_phase_mappings (
    id         UUID PRIMARY KEY,
    cluster    TEXT NOT NULL,
    kind       TEXT NOT NULL,
    phase      TEXT NOT NULL,
    status     TEXT NOT NULL CHECK (status IN ('pending', 'active', 'failed')),
    UNIQUE (cluster, kind, phase)
);
