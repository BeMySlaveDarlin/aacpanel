ALTER TABLE metrics_net_raw
    ADD COLUMN rx_total bigint,
    ADD COLUMN tx_total bigint;

CREATE TABLE metrics_net_1m (
    bucket   timestamptz NOT NULL,
    host_id  integer NOT NULL REFERENCES hosts (id),
    iface    text NOT NULL,
    rx_avg   bigint,
    rx_max   bigint,
    tx_avg   bigint,
    tx_max   bigint,
    rx_bytes bigint,
    tx_bytes bigint,
    samples  integer NOT NULL,
    PRIMARY KEY (host_id, iface, bucket)
) PARTITION BY RANGE (bucket);

CREATE TABLE metrics_net_1h (LIKE metrics_net_1m INCLUDING ALL) PARTITION BY RANGE (bucket);
ALTER TABLE metrics_net_1h ADD FOREIGN KEY (host_id) REFERENCES hosts (id);

UPDATE partition_config SET rollup_dep = 'net_1m' WHERE relname = 'metrics_net_raw';

INSERT INTO partition_config (relname, step_unit, keep, rollup_dep) VALUES
    ('metrics_net_1m', 'week',  '14 days', 'net_1h'),
    ('metrics_net_1h', 'month', '1 year',  NULL);

SELECT ensure_partitions('7 days');
