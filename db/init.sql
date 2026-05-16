CREATE TABLE IF NOT EXISTS temperatures (
    id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    sensor_id   VARCHAR(64)  NOT NULL,
    value       DECIMAL(5,2) NOT NULL,
    recorded_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS humidities (
    id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    sensor_id   VARCHAR(64)  NOT NULL,
    value       DECIMAL(5,2) NOT NULL,
    recorded_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS co2s (
    id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    sensor_id   VARCHAR(64)  NOT NULL,
    value       DECIMAL(7,2) NOT NULL,
    recorded_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS smells (
    id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    sensor_id   VARCHAR(64)  NOT NULL,
    value       DECIMAL(5,4) NOT NULL,
    recorded_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS smell_notifications (
    id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    sensor_id   VARCHAR(64)  NOT NULL,
    slack_ts    VARCHAR(32)  NULL,
    result      ENUM('unknown','confirmed','false_positive') NOT NULL DEFAULT 'unknown',
    notified_at DATETIME     NOT NULL
);

CREATE TABLE IF NOT EXISTS ble_rssi (
    id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    location    VARCHAR(50)  NOT NULL,
    rssi        INT          NOT NULL,
    recorded_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
);
