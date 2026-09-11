CREATE TABLE metrics_disk_1m (
    bucket    timestamptz NOT NULL,
    used_last bigint,
    used_max  bigint,
    used_avg  bigint,
    total     bigint,
    samples   integer NOT NULL,
    host_id   integer NOT NULL REFERENCES hosts (id),
    mount     text    NOT NULL,
    PRIMARY KEY (host_id, mount, bucket)
) PARTITION BY RANGE (bucket);

CREATE TABLE metrics_disk_1h (LIKE metrics_disk_1m INCLUDING ALL) PARTITION BY RANGE (bucket);
ALTER TABLE metrics_disk_1h ADD FOREIGN KEY (host_id) REFERENCES hosts (id);

CREATE TABLE ups_1m (
    bucket            timestamptz NOT NULL,
    charge_min        real,
    charge_avg        real,
    voltage_min       real,
    voltage_avg       real,
    input_voltage_min real,
    input_voltage_avg real,
    load_avg          real,
    load_max          real,
    on_battery_sec  integer NOT NULL DEFAULT 0,
    replace_battery boolean NOT NULL DEFAULT false,
    samples         integer NOT NULL,
    host_id         integer NOT NULL REFERENCES hosts (id),
    PRIMARY KEY (host_id, bucket)
) PARTITION BY RANGE (bucket);

CREATE TABLE ups_1h (LIKE ups_1m INCLUDING ALL) PARTITION BY RANGE (bucket);
ALTER TABLE ups_1h ADD FOREIGN KEY (host_id) REFERENCES hosts (id);

CREATE TABLE sessions_1m (
    bucket       timestamptz NOT NULL,
    tokens_max   bigint,
    pct_avg      real,
    pct_max      real,
    messages_max integer,
    samples      integer NOT NULL,
    host_id      integer NOT NULL REFERENCES hosts (id),
    name         text    NOT NULL,
    PRIMARY KEY (host_id, name, bucket)
) PARTITION BY RANGE (bucket);

CREATE TABLE sessions_1h (LIKE sessions_1m INCLUDING ALL) PARTITION BY RANGE (bucket);
ALTER TABLE sessions_1h ADD FOREIGN KEY (host_id) REFERENCES hosts (id);

ALTER TABLE metrics_container_1m ADD COLUMN disk_rw_min bigint, ADD COLUMN disk_rw_max bigint;
ALTER TABLE metrics_container_1h ADD COLUMN disk_rw_min bigint, ADD COLUMN disk_rw_max bigint;

INSERT INTO partition_config (relname, step_unit, keep, rollup_dep) VALUES
    ('metrics_disk_1m', 'week',  '14 days', 'disk_1h'),
    ('metrics_disk_1h', 'month', '1 year',  NULL),
    ('ups_1m',          'week',  '14 days', 'ups_1h'),
    ('ups_1h',          'month', '1 year',  NULL),
    ('sessions_1m',     'week',  '14 days', 'sessions_1h'),
    ('sessions_1h',     'month', '1 year',  NULL);

UPDATE partition_config SET keep = '2 days', rollup_dep = 'disk_1m'     WHERE relname = 'metrics_disk_raw';
UPDATE partition_config SET keep = '2 days', rollup_dep = 'ups_1m'      WHERE relname = 'ups_raw';
UPDATE partition_config SET keep = '2 days', rollup_dep = 'sessions_1m' WHERE relname = 'sessions_raw';

SELECT ensure_partitions('7 days');
