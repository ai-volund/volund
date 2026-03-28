-- Add unique index on pod_name for upsert-by-pod support.
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_instances_pod_name
    ON agent_instances (pod_name) WHERE pod_name IS NOT NULL;
