CREATE TABLE ksync_custom_resource_statuses (
    custom_resource_id UUID PRIMARY KEY REFERENCES ksync_custom_resources(id),
    status             TEXT        NOT NULL CHECK (status IN ('pending', 'active', 'failed')),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
