ALTER TABLE ksync_phase_mappings
    ADD COLUMN api_version TEXT NOT NULL DEFAULT '';

ALTER TABLE ksync_phase_mappings
    DROP CONSTRAINT ksync_phase_mappings_cluster_kind_phase_key;

ALTER TABLE ksync_phase_mappings
    ADD CONSTRAINT ksync_phase_mappings_cluster_api_version_kind_phase_key
    UNIQUE (cluster, api_version, kind, phase);
