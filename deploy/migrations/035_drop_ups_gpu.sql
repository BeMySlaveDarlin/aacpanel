DELETE FROM rules WHERE subject IN ('ups.on_battery', 'ups.charge_pct', 'ups.replace_battery');

DROP TABLE IF EXISTS ups_1h, ups_1m, ups_raw;
DROP TABLE IF EXISTS metrics_gpu_1h, metrics_gpu_1m, metrics_gpu_raw;

DELETE FROM partition_config WHERE relname IN
    ('ups_raw', 'ups_1m', 'ups_1h', 'metrics_gpu_raw', 'metrics_gpu_1m', 'metrics_gpu_1h');

DELETE FROM rollup_state WHERE name IN ('ups_1m', 'ups_1h', 'gpu_1m', 'gpu_1h');
