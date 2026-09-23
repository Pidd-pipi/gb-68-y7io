package database

import (
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"irrigation/internal/config"
	applogger "irrigation/pkg/logger"
)

var DB *gorm.DB

func Init() error {
	dsn := config.AppSettings.Postgres.DSN()

	var logLevel gormlogger.LogLevel
	if config.AppSettings.App.Env == "development" {
		logLevel = gormlogger.Info
	} else {
		logLevel = gormlogger.Error
	}

	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(logLevel),
	})

	if err != nil {
		applogger.Fatal("Failed to connect to database", zap.Error(err))
	}

	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := migrate(); err != nil {
		applogger.Error("Failed to run database migration", zap.Error(err))
		return err
	}

	applogger.Info("Database connected successfully")
	return nil
}

// migrate 幂等执行停灌功能相关的结构变更，兼容 init.sql 未覆盖的已有部署
func migrate() error {
	statements := []string{
		`ALTER TYPE trigger_type ADD VALUE IF NOT EXISTS 'emergency'`,
		`ALTER TABLE irrigation_logs ADD COLUMN IF NOT EXISTS remark TEXT`,
		`ALTER TABLE irrigation_zones ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`ALTER TABLE irrigation_schedules ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`,
		`DO $$ BEGIN
			CREATE TYPE suspension_status AS ENUM ('active', 'ended');
		EXCEPTION WHEN duplicate_object THEN NULL;
		END $$`,
		`CREATE TABLE IF NOT EXISTS zone_suspensions (
			id SERIAL PRIMARY KEY,
			zone_id INTEGER NOT NULL REFERENCES irrigation_zones(id),
			reason TEXT NOT NULL,
			expected_end_time TIMESTAMP NOT NULL,
			status suspension_status DEFAULT 'active',
			started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			ended_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_zone_suspensions_active ON zone_suspensions(zone_id) WHERE status = 'active'`,
		`CREATE INDEX IF NOT EXISTS idx_zone_suspensions_zone ON zone_suspensions(zone_id)`,
		`CREATE TABLE IF NOT EXISTS irrigation_skip_logs (
			id BIGSERIAL PRIMARY KEY,
			zone_id INTEGER NOT NULL REFERENCES irrigation_zones(id),
			suspension_id INTEGER NOT NULL REFERENCES zone_suspensions(id),
			schedule_id INTEGER REFERENCES irrigation_schedules(id),
			trigger_type trigger_type NOT NULL,
			reason TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_irrigation_skip_logs_zone_time ON irrigation_skip_logs(zone_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_irrigation_skip_logs_suspension ON irrigation_skip_logs(suspension_id)`,
	}

	for _, stmt := range statements {
		if err := DB.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}
