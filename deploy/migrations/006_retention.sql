ALTER TABLE partition_config
    ADD COLUMN keep       interval NOT NULL DEFAULT '2 days',
    ADD COLUMN rollup_dep text;

UPDATE partition_config SET keep = '14 days' WHERE relname LIKE '%\_1m';
UPDATE partition_config SET keep = '1 year'  WHERE relname LIKE '%\_1h';

UPDATE partition_config SET keep = '30 days'
WHERE relname IN ('metrics_disk_raw', 'ups_raw', 'sessions_raw');

UPDATE partition_config SET rollup_dep = 'container_1m' WHERE relname = 'metrics_container_raw';
UPDATE partition_config SET rollup_dep = 'host_1m'      WHERE relname = 'metrics_host_raw';
UPDATE partition_config SET rollup_dep = 'container_1h' WHERE relname = 'metrics_container_1m';
UPDATE partition_config SET rollup_dep = 'host_1h'      WHERE relname = 'metrics_host_1m';
