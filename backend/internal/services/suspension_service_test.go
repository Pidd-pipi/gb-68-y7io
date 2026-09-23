package services

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
	"irrigation/pkg/database"
)

const testPGPort = 55433

func setupTestDB(t *testing.T) {
	t.Helper()

	runtimePath := filepath.Join(os.TempDir(), "embedded-postgres-services")
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Port(testPGPort).
		Database("irrigation_test").
		Username("postgres").
		Password("postgres").
		RuntimePath(runtimePath))
	if err := pg.Start(); err != nil {
		t.Fatalf("failed to start embedded postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = pg.Stop()
	})

	dsn := "host=localhost port=55433 user=postgres password=postgres dbname=irrigation_test sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to connect embedded postgres: %v", err)
	}
	database.DB = db

	// 应用与 database/init.sql 一致的完整结构，验证建表脚本与迁移逻辑
	initSQL, err := os.ReadFile("../../../database/init.sql")
	if err != nil {
		t.Fatalf("failed to read init.sql: %v", err)
	}
	if err := db.Exec(string(initSQL)).Error; err != nil {
		t.Fatalf("failed to apply init.sql: %v", err)
	}
}

func mustCreateZone(t *testing.T, name string) models.IrrigationZone {
	t.Helper()
	zone := models.IrrigationZone{Name: name}
	if err := database.DB.Create(&zone).Error; err != nil {
		t.Fatalf("failed to create zone: %v", err)
	}
	return zone
}

func TestSuspensionLifecycle(t *testing.T) {
	setupTestDB(t)
	svc := NewSuspensionService()
	zone := mustCreateZone(t, "玫瑰园")

	end1 := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	s1, err := svc.SetSuspension(zone.ID, "区域管道检修", end1)
	if err != nil {
		t.Fatalf("SetSuspension failed: %v", err)
	}
	if s1.Status != models.SuspensionStatusActive {
		t.Fatalf("expected active status, got %s", s1.Status)
	}

	// 重复设置只更新同一条记录
	end2 := time.Now().Add(4 * time.Hour).Truncate(time.Second)
	s2, err := svc.SetSuspension(zone.ID, "暴雨红色预警", end2)
	if err != nil {
		t.Fatalf("SetSuspension (repeat) failed: %v", err)
	}
	if s2.ID != s1.ID {
		t.Fatalf("expected same record updated, got id %d != %d", s2.ID, s1.ID)
	}
	if s2.Reason != "暴雨红色预警" || !s2.ExpectedEndTime.Equal(end2) {
		t.Fatalf("record not updated: %+v", s2)
	}

	var activeCount int64
	database.DB.Model(&models.ZoneSuspension{}).
		Where("zone_id = ? AND status = ?", zone.ID, models.SuspensionStatusActive).
		Count(&activeCount)
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active suspension, got %d", activeCount)
	}

	// 当前生效的停灌可查询
	active, err := svc.GetActiveSuspension(zone.ID)
	if err != nil || active == nil {
		t.Fatalf("GetActiveSuspension failed: %v", err)
	}
	if active.Reason != "暴雨红色预警" {
		t.Fatalf("unexpected reason: %s", active.Reason)
	}

	// 记录跳过（定时计划 + 手动浇水）
	startTime := "06:00"
	schedule := models.IrrigationSchedule{
		Name:      "晨间定时浇水",
		Type:      models.ScheduleTypeTimed,
		ZoneID:    &zone.ID,
		Status:    models.ScheduleStatusActive,
		StartTime: &startTime,
	}
	if err := database.DB.Create(&schedule).Error; err != nil {
		t.Fatalf("failed to create schedule: %v", err)
	}
	if err := svc.RecordSkip(zone.ID, s1.ID, &schedule.ID, models.TriggerTypeTimed, active.Reason); err != nil {
		t.Fatalf("RecordSkip timed failed: %v", err)
	}
	if err := svc.RecordSkip(zone.ID, s1.ID, nil, models.TriggerTypeManual, active.Reason); err != nil {
		t.Fatalf("RecordSkip manual failed: %v", err)
	}

	skips, err := svc.ListRecentSkips(zone.ID, 10)
	if err != nil || len(skips) != 2 {
		t.Fatalf("expected 2 skip logs, got %d, err=%v", len(skips), err)
	}
	if skips[0].TriggerType != models.TriggerTypeManual || skips[1].TriggerType != models.TriggerTypeTimed {
		t.Fatalf("unexpected skip order/types: %+v", skips)
	}

	// 手动解除后不再生效，历史仍可查询
	if err := svc.ResumeSuspension(zone.ID); err != nil {
		t.Fatalf("ResumeSuspension failed: %v", err)
	}
	active, err = svc.GetActiveSuspension(zone.ID)
	if err != nil || active != nil {
		t.Fatalf("expected no active suspension after resume, got %+v, err=%v", active, err)
	}

	history, err := svc.GetSuspensionHistory(zone.ID, 50)
	if err != nil || len(history) != 1 {
		t.Fatalf("expected 1 history record, got %d, err=%v", len(history), err)
	}
	if history[0].Status != models.SuspensionStatusEnded || history[0].EndedAt == nil {
		t.Fatalf("expected ended history with ended_at, got %+v", history[0])
	}

	// 恢复后可再次设置，生成新记录
	s3, err := svc.SetSuspension(zone.ID, "二次检修", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("SetSuspension after resume failed: %v", err)
	}
	if s3.ID == s1.ID {
		t.Fatalf("expected new record after resume, got same id %d", s3.ID)
	}
	history, _ = svc.GetSuspensionHistory(zone.ID, 50)
	if len(history) != 2 {
		t.Fatalf("expected 2 history records, got %d", len(history))
	}
}

func TestSuspensionAutoExpiry(t *testing.T) {
	setupTestDB(t)
	svc := NewSuspensionService()
	zone := mustCreateZone(t, "草坪区")

	// 直接构造一条已到期的 active 记录（模拟后台任务到期解除前的状态）
	expired := models.ZoneSuspension{
		ZoneID:          zone.ID,
		Reason:          "短时暴雨",
		ExpectedEndTime: time.Now().Add(-time.Minute),
		Status:          models.SuspensionStatusActive,
		StartedAt:       time.Now().Add(-2 * time.Hour),
	}
	if err := database.DB.Create(&expired).Error; err != nil {
		t.Fatalf("failed to seed expired suspension: %v", err)
	}

	// 到期的停灌不应再被视为生效中
	active, err := svc.GetActiveSuspension(zone.ID)
	if err != nil || active != nil {
		t.Fatalf("expected expired suspension to be inactive, got %+v", active)
	}

	// 后台任务按预计时间解除
	affected, err := svc.EndExpiredSuspensions()
	if err != nil || affected != 1 {
		t.Fatalf("expected 1 expired suspension ended, got %d, err=%v", affected, err)
	}

	var updated models.ZoneSuspension
	database.DB.First(&updated, expired.ID)
	if updated.Status != models.SuspensionStatusEnded || updated.EndedAt == nil {
		t.Fatalf("expected ended status, got %+v", updated)
	}

	// 解除后可再次设置，生成新记录
	s, err := svc.SetSuspension(zone.ID, "继续检修", time.Now().Add(3*time.Hour))
	if err != nil {
		t.Fatalf("SetSuspension after expiry failed: %v", err)
	}
	var count int64
	database.DB.Model(&models.ZoneSuspension{}).Where("zone_id = ?", zone.ID).Count(&count)
	_ = s
	if count != 2 {
		t.Fatalf("expected 2 total records (ended + new active), got %d", count)
	}
}

// 已到期但后台任务尚未清扫时重复设置，应原地更新同一条记录，避免唯一索引冲突
func TestSetSuspensionOnExpiredUnswept(t *testing.T) {
	setupTestDB(t)
	svc := NewSuspensionService()
	zone := mustCreateZone(t, "花坛区")

	expired := models.ZoneSuspension{
		ZoneID:          zone.ID,
		Reason:          "短时暴雨",
		ExpectedEndTime: time.Now().Add(-time.Minute),
		Status:          models.SuspensionStatusActive,
		StartedAt:       time.Now().Add(-2 * time.Hour),
	}
	if err := database.DB.Create(&expired).Error; err != nil {
		t.Fatalf("failed to seed expired suspension: %v", err)
	}

	newEnd := time.Now().Add(5 * time.Hour).Truncate(time.Second)
	s, err := svc.SetSuspension(zone.ID, "暴雨持续，延长停灌", newEnd)
	if err != nil {
		t.Fatalf("SetSuspension on expired unswept record failed: %v", err)
	}
	if s.ID != expired.ID {
		t.Fatalf("expected same record updated, got id %d != %d", s.ID, expired.ID)
	}

	var count int64
	database.DB.Model(&models.ZoneSuspension{}).Where("zone_id = ?", zone.ID).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 record updated in place, got %d", count)
	}

	active, err := svc.GetActiveSuspension(zone.ID)
	if err != nil || active == nil || active.Reason != "暴雨持续，延长停灌" {
		t.Fatalf("expected renewed active suspension, got %+v, err=%v", active, err)
	}
}

func TestEmergencyIrrigationDuringSuspension(t *testing.T) {
	setupTestDB(t)
	suspensionSvc := NewSuspensionService()
	irrigationSvc := NewIrrigationService()
	zone := mustCreateZone(t, "灌木区")

	s, err := suspensionSvc.SetSuspension(zone.ID, "设备检修", time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("SetSuspension failed: %v", err)
	}

	// 紧急启动不受停灌限制，书面原因留存在 remark
	log, err := irrigationSvc.StartEmergencyIrrigation(zone.ID, "主管道爆裂已断水，恢复后需立即补水")
	if err != nil {
		t.Fatalf("StartEmergencyIrrigation failed: %v", err)
	}
	if log.TriggerType != models.TriggerTypeEmergency {
		t.Fatalf("expected emergency trigger type, got %s", log.TriggerType)
	}
	if log.Remark == nil || *log.Remark == "" {
		t.Fatalf("expected remark to keep written reason, got %+v", log.Remark)
	}

	// 原停灌状态不受影响，仍按预计时间生效
	active, err := suspensionSvc.GetActiveSuspension(zone.ID)
	if err != nil || active == nil || active.ID != s.ID {
		t.Fatalf("suspension should remain active after emergency irrigation, got %+v", active)
	}

	// 历史记录中可查询到紧急启动记录
	logs, err := irrigationSvc.GetIrrigationHistory(&zone.ID, time.Time{}, time.Time{}, 10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("expected 1 irrigation log, got %d, err=%v", len(logs), err)
	}
	if logs[0].TriggerType != models.TriggerTypeEmergency {
		t.Fatalf("unexpected trigger type in history: %s", logs[0].TriggerType)
	}
}

func TestSetSuspensionOnMissingZone(t *testing.T) {
	setupTestDB(t)
	svc := NewSuspensionService()

	_, err := svc.SetSuspension(9999, "检修", time.Now().Add(time.Hour))
	if err == nil || err.Error() != "zone not found" {
		t.Fatalf("expected zone not found error, got %v", err)
	}
}
