ALTER TABLE metrics_host_raw
    ADD COLUMN cpu_temp  real,
    ADD COLUMN mem_temp  real,
    ADD COLUMN disk_temp real;

ALTER TABLE metrics_host_1m
    ADD COLUMN cpu_temp_avg  real,
    ADD COLUMN cpu_temp_max  real,
    ADD COLUMN mem_temp_avg  real,
    ADD COLUMN mem_temp_max  real,
    ADD COLUMN disk_temp_avg real,
    ADD COLUMN disk_temp_max real;

ALTER TABLE metrics_host_1h
    ADD COLUMN cpu_temp_avg  real,
    ADD COLUMN cpu_temp_max  real,
    ADD COLUMN mem_temp_avg  real,
    ADD COLUMN mem_temp_max  real,
    ADD COLUMN disk_temp_avg real,
    ADD COLUMN disk_temp_max real;
