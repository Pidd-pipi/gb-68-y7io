package database

import (
	"os"
	"path/filepath"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// 旧版结构（停灌功能上线前）：trigger_type 无 emergency，irrigation_logs 无 remark，无停灌相关表
const legacySchema = `
CREATE TABLE IF NOT EXISTS irrigation_zones (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TYPE device_type AS ENUM ('valve', 'pump', 'soil_sensor', 'rain_sensor', 'temp_sensor');
CREATE TYPE device_status AS ENUM ('online', 'offline', 'error');
CREATE TABLE IF NOT EXISTS devices (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    type device_type NOT NULL,
    serial_number VARCHAR(100) UNIQUE NOT NULL,
    zone_id INTEGER REFERENCES irrigation_zones(id),
    status device_status DEFAULT 'offline',
    last_heartbeat TIMESTAMP,
    config JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TYPE schedule_type AS ENUM ('timed', 'conditional');
CREATE TYPE schedule_status AS ENUM ('active', 'inactive');
CREATE TYPE repeat_mode AS ENUM ('once', 'daily', 'weekly', 'monthly');
CREATE TABLE IF NOT EXISTS irrigation_schedules (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    type schedule_type NOT NULL,
    zone_id INTEGER REFERENCES irrigation_zones(id),
    status schedule_status DEFAULT 'inactive',
    start_time TIME,
    duration INTEGER,
    repeat_mode repeat_mode DEFAULT 'once',
    repeat_days INTEGER[],
    humidity_threshold DECIMAL(5, 2),
    rain_sensor_id INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TYPE trigger_type AS ENUM ('manual', 'timed', 'conditional');
CREATE TYPE execution_status AS ENUM ('success', 'failed', 'in_progress');
CREATE TABLE IF NOT EXISTS irrigation_logs (
    id BIGSERIAL PRIMARY KEY,
    schedule_id INTEGER REFERENCES irrigation_schedules(id),
    zone_id INTEGER REFERENCES irrigation_zones(id),
    trigger_type trigger_type NOT NULL,
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP,
    duration INTEGER,
    water_usage DECIMAL(10, 2),
    status execution_status NOT NULL,
    error_message TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
`

func setupLegacyDB(t *testing.T) {
	t.Helper()

	runtimePath := filepath.Join(os.TempDir(), "embedded-postgres-database")
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Port(55434).
		Database("irrigation_legacy").
		Username("postgres").
		Password("postgres").
		RuntimePath(runtimePath))
	if err := pg.Start(); err != nil {
		t.Fatalf("failed to start embedded postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = pg.Stop()
	})

	dsn := "host=localhost port=55434 user=postgres password=postgres dbname=irrigation_legacy sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to connect embedded postgres: %v", err)
	}
	DB = db

	if err := DB.Exec(legacySchema).Error; err != nil {
		t.Fatalf("failed to apply legacy schema: %v", err)
	}
}

func TestMigrateUpgradesLegacySchema(t *testing.T) {
	setupLegacyDB(t)

	if err := migrate(); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	// 幂等：重复执行不报错
	if err := migrate(); err != nil {
		t.Fatalf("migrate should be idempotent: %v", err)
	}

	// trigger_type 已包含 emergency
	var enumCount int64
	DB.Raw(`SELECT COUNT(*) FROM pg_enum e JOIN pg_type t ON e.enumtypid = t.oid WHERE t.typname = 'trigger_type' AND e.enumlabel = 'emergency'`).Scan(&enumCount)
	if enumCount != 1 {
		t.Fatalf("expected emergency in trigger_type enum")
	}

	// irrigation_logs 已有 remark 列
	var colCount int64
	DB.Raw(`SELECT COUNT(*) FROM information_schema.columns WHERE table_name = 'irrigation_logs' AND column_name = 'remark'`).Scan(&colCount)
	if colCount != 1 {
		t.Fatalf("expected remark column on irrigation_logs")
	}

	// 停灌相关表已创建且可写入
	if err := DB.Exec(`INSERT INTO irrigation_zones (name) VALUES ('迁移测试区')`).Error; err != nil {
		t.Fatalf("failed to insert zone: %v", err)
	}
	if err := DB.Exec(`INSERT INTO zone_suspensions (zone_id, reason, expected_end_time) VALUES (1, '检修', NOW() + INTERVAL '1 hour')`).Error; err != nil {
		t.Fatalf("failed to insert zone_suspension: %v", err)
	}
	if err := DB.Exec(`INSERT INTO irrigation_skip_logs (zone_id, suspension_id, trigger_type, reason) VALUES (1, 1, 'timed', '检修')`).Error; err != nil {
		t.Fatalf("failed to insert irrigation_skip_log: %v", err)
	}
	if err := DB.Exec(`INSERT INTO irrigation_logs (zone_id, trigger_type, start_time, status, remark) VALUES (1, 'emergency', NOW(), 'in_progress', '设备漏水')`).Error; err != nil {
		t.Fatalf("failed to insert emergency irrigation_log: %v", err)
	}

	// 同一区域仅允许一条 active 停灌
	if err := DB.Exec(`INSERT INTO zone_suspensions (zone_id, reason, expected_end_time) VALUES (1, '重复', NOW() + INTERVAL '2 hours')`).Error; err == nil {
		t.Fatalf("expected unique index violation for second active suspension")
	}
}
