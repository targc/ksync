CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE ksync_custom_resources (
    id                                UUID        PRIMARY KEY,
    project                           TEXT        NOT NULL DEFAULT '',
    cluster                           TEXT        NOT NULL DEFAULT '',
    api_version                       TEXT        NOT NULL DEFAULT '',
    kind                              TEXT        NOT NULL DEFAULT '',
    namespace                         TEXT        NOT NULL DEFAULT '',
    name                              TEXT        NOT NULL DEFAULT '',
    json                              JSONB,
    syncing_change_custom_resource_id UUID,       -- FK added after ksync_change_custom_resources is created
    last_change_custom_resource_id    UUID,
    last_sync_error                   TEXT,
    created_at                        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at                        TIMESTAMPTZ
);

CREATE TABLE ksync_change_custom_resources (
    id                 UUID        PRIMARY KEY,
    custom_resource_id UUID        NOT NULL REFERENCES ksync_custom_resources(id),
    json               JSONB,
    action             TEXT        NOT NULL CHECK (action IN ('apply', 'delete')),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- close the circular FK now that both tables exist
ALTER TABLE ksync_custom_resources
    ADD CONSTRAINT fk_ksync_custom_resources_syncing_change
    FOREIGN KEY (syncing_change_custom_resource_id)
    REFERENCES ksync_change_custom_resources(id)
    ON DELETE SET NULL;

CREATE INDEX idx_ksync_custom_resources_cluster    ON ksync_custom_resources(cluster);
CREATE INDEX idx_ksync_custom_resources_project    ON ksync_custom_resources(project);
CREATE INDEX idx_ksync_custom_resources_kind       ON ksync_custom_resources(kind);
CREATE INDEX idx_ksync_custom_resources_deleted_at ON ksync_custom_resources(deleted_at);

CREATE INDEX idx_ksync_change_custom_resources_cr_id ON ksync_change_custom_resources(custom_resource_id);
