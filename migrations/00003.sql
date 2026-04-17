-- Enforce that (cluster, api_version, kind, namespace, name) is unique among active (non-deleted) resources.
-- Soft-deleted rows are excluded so the same identity can be re-created after deletion.
CREATE UNIQUE INDEX ksync_custom_resources_identity_unique
    ON ksync_custom_resources (cluster, api_version, kind, namespace, name)
    WHERE deleted_at IS NULL;
