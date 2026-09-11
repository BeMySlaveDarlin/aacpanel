DROP INDEX metrics_container_1m_top;

CREATE INDEX metrics_container_1m_top ON metrics_container_1m (host_id, bucket DESC);
