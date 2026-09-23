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

// SetSuspension 设置区域停灌。同一区域已有生效中的停灌时，只更新该记录的原因和预计结束时间。
func (s *SuspensionService) SetSuspension(zoneID uint, reason string, expectedEndAt time.Time) (*models.IrrigationSuspension, error) {
	var zone models.IrrigationZone
	if err := database.DB.First(&zone, zoneID).Error; err != nil {
		return nil, errors.New("zone not found")
	}

	suspension, err := s.GetActiveSuspension(zoneID)
	if err != nil {
		return nil, err
	}

	if suspension != nil {
		updates := map[string]interface{}{
			"reason":          reason,
			"expected_end_at": expectedEndAt,
		}
		if err := database.DB.Model(suspension).Updates(updates).Error; err != nil {
			return nil, err
		}
		suspension.Reason = reason
		suspension.ExpectedEndAt = expectedEndAt
		return suspension, nil
	}

	suspension = &models.IrrigationSuspension{
		ZoneID:        zoneID,
		Reason:        reason,
		ExpectedEndAt: expectedEndAt,
		Status:        models.SuspensionStatusActive,
	}
	if err := database.DB.Create(suspension).Error; err != nil {
		return nil, err
	}
	return suspension, nil
}

// GetActiveSuspension 返回区域当前生效中的停灌记录；已到达预计结束时间的记录会被自动解除。
func (s *SuspensionService) GetActiveSuspension(zoneID uint) (*models.IrrigationSuspension, error) {
	var suspension models.IrrigationSuspension
	err := database.DB.
		Where("zone_id = ? AND status = ?", zoneID, models.SuspensionStatusActive).
		Order("created_at DESC").
		First(&suspension).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// 到达预计结束时间，按预计时间自动解除
	if !suspension.ExpectedEndAt.After(time.Now()) {
		liftedAt := suspension.ExpectedEndAt
		if err := database.DB.Model(&suspension).Updates(map[string]interface{}{
			"status":    models.SuspensionStatusLifted,
			"lifted_at": liftedAt,
		}).Error; err != nil {
			return nil, err
		}
		return nil, nil
	}

	return &suspension, nil
}

// LiftSuspension 提前手动解除区域停灌。
func (s *SuspensionService) LiftSuspension(zoneID uint) error {
	now := time.Now()
	result := database.DB.Model(&models.IrrigationSuspension{}).
		Where("zone_id = ? AND status = ?", zoneID, models.SuspensionStatusActive).
		Updates(map[string]interface{}{
			"status":    models.SuspensionStatusLifted,
			"lifted_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("no active suspension")
	}
	return nil
}

// ListSuspensions 查询区域停灌历史（含已解除的记录），按创建时间倒序。
func (s *SuspensionService) ListSuspensions(zoneID uint, limit int) ([]models.IrrigationSuspension, error) {
	// 先触发一次到期解除，保证返回的状态是最新的
	if _, err := s.GetActiveSuspension(zoneID); err != nil {
		return nil, err
	}

	var suspensions []models.IrrigationSuspension
	query := database.DB.Where("zone_id = ?", zoneID).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&suspensions).Error; err != nil {
		return nil, err
	}
	return suspensions, nil
}
