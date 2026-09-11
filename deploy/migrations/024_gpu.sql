CREATE TABLE metrics_gpu_raw (
    ts         timestamptz NOT NULL,
    host_id    integer NOT NULL REFERENCES hosts (id),
    idx        smallint NOT NULL,
    name       text NOT NULL,
    util_pct   real,
    mem_used   bigint,
    mem_total  bigint,
    temp_c     real,
    power_w    real,
    fan_pct    real,
    PRIMARY KEY (host_id, idx, ts)
) PARTITION BY RANGE (ts);

CREATE TABLE metrics_gpu_1m (
    bucket        timestamptz NOT NULL,
    host_id       integer NOT NULL REFERENCES hosts (id),
    idx           smallint NOT NULL,
    name          text NOT NULL,
    util_avg      real,
    util_max      real,
    mem_used_avg  bigint,
    mem_used_max  bigint,
    mem_total     bigint,
    temp_avg      real,
    temp_max      real,
    power_avg     real,
    power_max     real,
    samples       integer NOT NULL,
    PRIMARY KEY (host_id, idx, bucket)
) PARTITION BY RANGE (bucket);

CREATE TABLE metrics_gpu_1h (LIKE metrics_gpu_1m INCLUDING ALL) PARTITION BY RANGE (bucket);
ALTER TABLE metrics_gpu_1h ADD FOREIGN KEY (host_id) REFERENCES hosts (id);

INSERT INTO partition_config (relname, step_unit, keep, rollup_dep) VALUES
    ('metrics_gpu_raw', 'day',   '2 days',  'gpu_1m'),
    ('metrics_gpu_1m',  'week',  '14 days', 'gpu_1h'),
    ('metrics_gpu_1h',  'month', '1 year',  NULL);

SELECT ensure_partitions('7 days');
