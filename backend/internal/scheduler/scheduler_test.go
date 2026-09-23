package scheduler

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"irrigation/internal/models"
	"irrigation/internal/services"
	"irrigation/pkg/database"
	"irrigation/pkg/logger"
)

func setupSchedulerTestDB(t *testing.T) {
	t.Helper()

	logger.Init("error")

	runtimePath := filepath.Join(os.TempDir(), "embedded-postgres-scheduler")
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Port(55436).
		Database("irrigation_scheduler_test").
		Username("postgres").
		Password("postgres").
		RuntimePath(runtimePath))
	if err := pg.Start(); err != nil {
		t.Fatalf("failed to start embedded postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = pg.Stop()
	})

	dsn := "host=localhost port=55436 user=postgres password=postgres dbname=irrigation_scheduler_test sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to connect embedded postgres: %v", err)
	}
	database.DB = db

	initSQL, err := os.ReadFile("../../../database/init.sql")
	if err != nil {
		t.Fatalf("failed to read init.sql: %v", err)
	}
	if err := db.Exec(string(initSQL)).Error; err != nil {
		t.Fatalf("failed to apply init.sql: %v", err)
	}
}

func TestSchedulerSkipsSuspendedZone(t *testing.T) {
	setupSchedulerTestDB(t)

	zone := models.IrrigationZone{Name: "调度测试区"}
	if err := database.DB.Create(&zone).Error; err != nil {
		t.Fatalf("failed to create zone: %v", err)
	}

	// 定时计划：当前时间到点
	now := time.Now()
	startTime := now.Format("15:04")
	timedSchedule := models.IrrigationSchedule{
		Name:       "到点定时浇水",
		Type:       models.ScheduleTypeTimed,
		ZoneID:     &zone.ID,
		Status:     models.ScheduleStatusActive,
		StartTime:  &startTime,
		RepeatMode: models.RepeatModeDaily,
	}
	if err := database.DB.Create(&timedSchedule).Error; err != nil {
		t.Fatalf("failed to create timed schedule: %v", err)
	}

	// 条件计划：湿度低于阈值
	threshold := 40.0
	condSchedule := models.IrrigationSchedule{
		Name:              "低湿度自动浇水",
		Type:              models.ScheduleTypeConditional,
		ZoneID:            &zone.ID,
		Status:            models.ScheduleStatusActive,
		HumidityThreshold: &threshold,
	}
	if err := database.DB.Create(&condSchedule).Error; err != nil {
		t.Fatalf("failed to create conditional schedule: %v", err)
	}

	// 土壤湿度传感器数据（低于阈值，条件计划应触发）
	sensor := models.Device{
		Name:         "土壤湿度计",
		Type:         models.DeviceTypeSoilSensor,
		SerialNumber: "SOIL-TEST-001",
		ZoneID:       &zone.ID,
	}
	if err := database.DB.Create(&sensor).Error; err != nil {
		t.Fatalf("failed to create sensor: %v", err)
	}
	humidity := models.SensorData{
		DeviceID:  sensor.ID,
		DataType:  "humidity",
		Value:     25.0,
		Unit:      "%",
		Timestamp: time.Now(),
	}
	if err := database.DB.Create(&humidity).Error; err != nil {
		t.Fatalf("failed to create sensor data: %v", err)
	}

	// 设置停灌
	suspensionSvc := services.NewSuspensionService()
	if _, err := suspensionSvc.SetSuspension(zone.ID, "暴雨红色预警", time.Now().Add(2*time.Hour)); err != nil {
		t.Fatalf("SetSuspension failed: %v", err)
	}

	sched := NewIrrigationScheduler()
	sched.executeScheduleIfNeeded(timedSchedule)
	sched.executeScheduleIfNeeded(condSchedule)

	// 两种触发方式都被跳过并留下记录
	var skips []models.IrrigationSkipLog
	if err := database.DB.Where("zone_id = ?", zone.ID).Order("id").Find(&skips).Error; err != nil {
		t.Fatalf("failed to query skips: %v", err)
	}
	if len(skips) != 2 {
		t.Fatalf("expected 2 skip records, got %d", len(skips))
	}
	if skips[0].TriggerType != models.TriggerTypeTimed || *skips[0].ScheduleID != timedSchedule.ID {
		t.Fatalf("unexpected timed skip: %+v", skips[0])
	}
	if skips[1].TriggerType != models.TriggerTypeConditional || *skips[1].ScheduleID != condSchedule.ID {
		t.Fatalf("unexpected conditional skip: %+v", skips[1])
	}
	if skips[0].Reason != "暴雨红色预警" {
		t.Fatalf("skip should carry suspension reason: %+v", skips[0])
	}

	// 停灌期间不产生任何灌溉执行记录
	var logCount int64
	database.DB.Model(&models.IrrigationLog{}).Where("zone_id = ?", zone.ID).Count(&logCount)
	if logCount != 0 {
		t.Fatalf("expected no irrigation logs during suspension, got %d", logCount)
	}

	// 解除停灌后定时计划恢复执行
	if err := suspensionSvc.ResumeSuspension(zone.ID); err != nil {
		t.Fatalf("ResumeSuspension failed: %v", err)
	}
	sched.executeScheduleIfNeeded(timedSchedule)

	deadline := time.Now().Add(3 * time.Second)
	for {
		database.DB.Model(&models.IrrigationLog{}).Where("zone_id = ?", zone.ID).Count(&logCount)
		if logCount > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected irrigation log after resume")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
