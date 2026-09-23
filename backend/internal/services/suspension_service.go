package services

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"irrigation/internal/models"
	"irrigation/pkg/database"
)

type SuspensionService struct{}

func NewSuspensionService() *SuspensionService {
	return &SuspensionService{}
}

// SetSuspension 设置区域临时停灌；若该区域已存在停灌记录（含已到期待解除的），
// 则更新同一条记录，保证同一区域只有一条生效中的停灌状态
func (s *SuspensionService) SetSuspension(zoneID uint, reason string, expectedEndTime time.Time) (*models.ZoneSuspension, error) {
	var zone models.IrrigationZone
	if err := database.DB.First(&zone, zoneID).Error; err != nil {
		return nil, errors.New("zone not found")
	}

	var existing models.ZoneSuspension
	err := database.DB.
		Where("zone_id = ? AND status = ?", zoneID, models.SuspensionStatusActive).
		Order("id DESC").
		First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	if err == nil {
		updates := map[string]interface{}{
			"reason":            reason,
			"expected_end_time": expectedEndTime,
		}
		if err := database.DB.Model(&existing).Updates(updates).Error; err != nil {
			return nil, err
		}
		existing.Reason = reason
		existing.ExpectedEndTime = expectedEndTime
		return &existing, nil
	}

	suspension := &models.ZoneSuspension{
		ZoneID:          zoneID,
		Reason:          reason,
		ExpectedEndTime: expectedEndTime,
		Status:          models.SuspensionStatusActive,
		StartedAt:       time.Now(),
	}
	if err := database.DB.Create(suspension).Error; err != nil {
		return nil, err
	}
	return suspension, nil
}

// ResumeSuspension 手动解除区域停灌，历史记录保留可查询
func (s *SuspensionService) ResumeSuspension(zoneID uint) error {
	now := time.Now()
	result := database.DB.Model(&models.ZoneSuspension{}).
		Where("zone_id = ? AND status = ?", zoneID, models.SuspensionStatusActive).
		Updates(map[string]interface{}{
			"status":   models.SuspensionStatusEnded,
			"ended_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("no active suspension for this zone")
	}
	return nil
}

// GetActiveSuspension 获取区域当前生效的停灌记录（未到期），无则返回 nil
func (s *SuspensionService) GetActiveSuspension(zoneID uint) (*models.ZoneSuspension, error) {
	var suspension models.ZoneSuspension
	err := database.DB.
		Where("zone_id = ? AND status = ? AND expected_end_time > ?", zoneID, models.SuspensionStatusActive, time.Now()).
		Order("id DESC").
		First(&suspension).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &suspension, nil
}

// GetSuspensionHistory 获取区域停灌历史（含已解除/已到期的记录）
func (s *SuspensionService) GetSuspensionHistory(zoneID uint, limit int) ([]models.ZoneSuspension, error) {
	var suspensions []models.ZoneSuspension
	query := database.DB.Where("zone_id = ?", zoneID).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&suspensions).Error; err != nil {
		return nil, err
	}
	return suspensions, nil
}

// RecordSkip 记录一次因停灌被跳过的灌溉
func (s *SuspensionService) RecordSkip(zoneID uint, suspensionID uint, scheduleID *uint, triggerType models.TriggerType, reason string) error {
	skip := &models.IrrigationSkipLog{
		ZoneID:       zoneID,
		SuspensionID: suspensionID,
		ScheduleID:   scheduleID,
		TriggerType:  triggerType,
		Reason:       reason,
	}
	return database.DB.Create(skip).Error
}

// ListRecentSkips 获取区域最近的跳过记录
func (s *SuspensionService) ListRecentSkips(zoneID uint, limit int) ([]models.IrrigationSkipLog, error) {
	var skips []models.IrrigationSkipLog
	query := database.DB.Where("zone_id = ?", zoneID).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&skips).Error; err != nil {
		return nil, err
	}
	return skips, nil
}

// EndExpiredSuspensions 将已到预计结束时间的停灌记录标记为解除
func (s *SuspensionService) EndExpiredSuspensions() (int64, error) {
	now := time.Now()
	result := database.DB.Model(&models.ZoneSuspension{}).
		Where("status = ? AND expected_end_time <= ?", models.SuspensionStatusActive, now).
		Updates(map[string]interface{}{
			"status":   models.SuspensionStatusEnded,
			"ended_at": now,
		})
	return result.RowsAffected, result.Error
}
