CREATE TABLE partition_config (
    relname   text PRIMARY KEY,
    step_unit text NOT NULL CHECK (step_unit IN ('day', 'week', 'month'))
);

CREATE FUNCTION ensure_partitions(p_ahead interval DEFAULT '7 days')
RETURNS integer
LANGUAGE plpgsql
SET timezone = 'UTC'
SET search_path = public, pg_temp
AS $$
DECLARE
    cfg    record;
    v_step interval;
    v_from timestamptz;
    v_to   timestamptz;
    v_name text;
    v_made integer := 0;
BEGIN
    FOR cfg IN SELECT relname, step_unit FROM partition_config ORDER BY relname LOOP
        v_step := ('1 ' || cfg.step_unit)::interval;
        v_from := date_trunc(cfg.step_unit, now());
        WHILE v_from <= now() + p_ahead LOOP
            v_to   := v_from + v_step;

            v_name := cfg.relname || '_' || to_char(v_from, 'YYYYMMDD');
            IF to_regclass('public.' || quote_ident(v_name)) IS NULL THEN
                EXECUTE format(
                    'CREATE TABLE %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
                    v_name, cfg.relname, v_from, v_to);
                v_made := v_made + 1;
            END IF;
            v_from := v_to;
        END LOOP;
    END LOOP;
    RETURN v_made;
END;
$$;

CREATE TABLE metrics_container_raw (
    ts          timestamptz NOT NULL,
    mem_bytes   bigint,
    disk_rw     bigint,
    disk_rootfs bigint,
    cpu_pct     real,
    mem_pct     real,
    host_id     integer NOT NULL REFERENCES hosts (id),
    container   text    NOT NULL,
    state       text,
    PRIMARY KEY (host_id, container, ts)
) PARTITION BY RANGE (ts);

CREATE INDEX metrics_container_raw_top ON metrics_container_raw (host_id, ts DESC)
    INCLUDE (container, cpu_pct, mem_bytes);

CREATE TABLE metrics_container_1m (
    bucket    timestamptz NOT NULL,
    mem_avg   bigint,
    mem_max   bigint,
    cpu_avg   real,
    cpu_max   real,
    samples   integer NOT NULL,
    host_id   integer NOT NULL REFERENCES hosts (id),
    container text    NOT NULL,
    PRIMARY KEY (host_id, container, bucket)
) PARTITION BY RANGE (bucket);

CREATE INDEX metrics_container_1m_top ON metrics_container_1m (host_id, bucket DESC)
    INCLUDE (container, cpu_avg, cpu_max, mem_avg, mem_max);

CREATE TABLE metrics_container_1h (
    bucket    timestamptz NOT NULL,
    mem_avg   bigint,
    mem_max   bigint,
    cpu_avg   real,
    cpu_max   real,
    samples   integer NOT NULL,
    host_id   integer NOT NULL REFERENCES hosts (id),
    container text    NOT NULL,
    PRIMARY KEY (host_id, container, bucket)
) PARTITION BY RANGE (bucket);

CREATE INDEX metrics_container_1h_top ON metrics_container_1h (host_id, bucket DESC)
    INCLUDE (container, cpu_avg, cpu_max, mem_avg, mem_max);

CREATE TABLE metrics_host_raw (
    ts        timestamptz NOT NULL,
    mem_used  bigint,
    mem_total bigint,
    swap_used bigint,
    cpu_pct   real,
    load1     real,
    host_id   integer NOT NULL REFERENCES hosts (id),
    PRIMARY KEY (host_id, ts)
) PARTITION BY RANGE (ts);

CREATE TABLE metrics_host_1m (
    bucket        timestamptz NOT NULL,
    mem_used_avg  bigint,
    mem_used_max  bigint,
    mem_total     bigint,
    swap_used_avg bigint,
    swap_used_max bigint,
    cpu_avg       real,
    cpu_max       real,
    load1_avg     real,
    load1_max     real,
    samples       integer NOT NULL,
    host_id       integer NOT NULL REFERENCES hosts (id),
    PRIMARY KEY (host_id, bucket)
) PARTITION BY RANGE (bucket);

CREATE TABLE metrics_host_1h (
    bucket        timestamptz NOT NULL,
    mem_used_avg  bigint,
    mem_used_max  bigint,
    mem_total     bigint,
    swap_used_avg bigint,
    swap_used_max bigint,
    cpu_avg       real,
    cpu_max       real,
    load1_avg     real,
    load1_max     real,
    samples       integer NOT NULL,
    host_id       integer NOT NULL REFERENCES hosts (id),
    PRIMARY KEY (host_id, bucket)
) PARTITION BY RANGE (bucket);

CREATE TABLE metrics_disk_raw (
    ts      timestamptz NOT NULL,
    used    bigint,
    total   bigint,
    host_id integer NOT NULL REFERENCES hosts (id),
    mount   text    NOT NULL,
    PRIMARY KEY (host_id, mount, ts)
) PARTITION BY RANGE (ts);

CREATE TABLE metrics_net_raw (
    ts      timestamptz NOT NULL,
    rx_rate bigint,
    tx_rate bigint,
    host_id integer NOT NULL REFERENCES hosts (id),
    iface   text    NOT NULL,
    PRIMARY KEY (host_id, iface, ts)
) PARTITION BY RANGE (ts);

CREATE TABLE ups_raw (
    ts            timestamptz NOT NULL,
    charge        real,
    voltage       real,
    input_voltage real,
    load_pct      real,
    host_id       integer NOT NULL REFERENCES hosts (id),
    on_battery    boolean NOT NULL,
    flags         text[],
    PRIMARY KEY (host_id, ts)
) PARTITION BY RANGE (ts);

CREATE TABLE sessions_raw (
    ts       timestamptz NOT NULL,
    tokens   bigint,
    pct      real,
    messages integer,
    host_id  integer NOT NULL REFERENCES hosts (id),
    name     text    NOT NULL,
    project  text,
    model    text,
    PRIMARY KEY (host_id, name, ts)
) PARTITION BY RANGE (ts);

INSERT INTO partition_config (relname, step_unit) VALUES
    ('metrics_container_raw', 'day'),
    ('metrics_container_1m',  'week'),
    ('metrics_container_1h',  'month'),
    ('metrics_host_raw',      'day'),
    ('metrics_host_1m',       'week'),
    ('metrics_host_1h',       'month'),
    ('metrics_disk_raw',      'day'),
    ('metrics_net_raw',       'day'),
    ('ups_raw',               'day'),
    ('sessions_raw',          'day');

SELECT ensure_partitions('7 days');
