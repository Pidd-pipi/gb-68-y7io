package controllers

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"irrigation/internal/models"
	"irrigation/internal/services"
	"irrigation/pkg/response"
)

type IrrigationController struct {
	irrigationService *services.IrrigationService
	suspensionService *services.SuspensionService
}

func NewIrrigationController() *IrrigationController {
	return &IrrigationController{
		irrigationService: services.NewIrrigationService(),
		suspensionService: services.NewSuspensionService(),
	}
}

// ManualIrrigationResult 手动灌溉结果
type ManualIrrigationResult struct {
	Skipped    bool                         `json:"skipped"`
	Suspension *models.IrrigationSuspension `json:"suspension,omitempty"`
	Log        *models.IrrigationLog        `json:"log,omitempty"`
}

// ManualIrrigate godoc
// @Summary 手动灌溉
// @Description 触发手动灌溉。区域停灌期间普通手动灌溉会被跳过并留下跳过记录；紧急情况（如已断水、设备漏水）填写书面原因 emergency_reason 后可立即启动，原停灌状态仍按预计时间解除
// @Tags 灌溉执行
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body object true "手动灌溉请求" {"zone_id": 1, "emergency_reason": "设备漏水需紧急冲管"}
// @Success 200 {object} controllers.ManualIrrigationResult
// @Router /api/irrigation/manual [post]
func (c *IrrigationController) ManualIrrigate(ctx *gin.Context) {
	var req struct {
		ZoneID          uint   `json:"zone_id" binding:"required"`
		EmergencyReason string `json:"emergency_reason"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.BadRequest(ctx, "Invalid request body")
		return
	}

	suspension, err := c.suspensionService.GetActiveSuspension(req.ZoneID)
	if err != nil {
		response.InternalServerError(ctx, err.Error())
		return
	}

	if suspension != nil {
		emergencyReason := strings.TrimSpace(req.EmergencyReason)
		if emergencyReason == "" {
			// 停灌期间普通手动灌溉直接跳过，并留下跳过记录
			note := "区域停灌中，跳过手动灌溉；停灌原因：" + suspension.Reason
			log, err := c.irrigationService.RecordSkip(nil, &req.ZoneID, models.TriggerTypeManual, note)
			if err != nil {
				response.InternalServerError(ctx, err.Error())
				return
			}
			response.Success(ctx, ManualIrrigationResult{
				Skipped:    true,
				Suspension: suspension,
				Log:        log,
			})
			return
		}

		// 紧急情况：填写书面原因后立即启动，停灌状态保持至预计结束时间自动解除
		note := "紧急启动（停灌期间），书面原因：" + emergencyReason
		log, err := c.irrigationService.StartIrrigation(nil, &req.ZoneID, models.TriggerTypeManual, note)
		if err != nil {
			response.InternalServerError(ctx, err.Error())
			return
		}
		response.Success(ctx, ManualIrrigationResult{
			Skipped:    false,
			Suspension: suspension,
			Log:        log,
		})
		return
	}

	log, err := c.irrigationService.StartIrrigation(nil, &req.ZoneID, models.TriggerTypeManual, "")
	if err != nil {
		response.InternalServerError(ctx, err.Error())
		return
	}

	response.Success(ctx, ManualIrrigationResult{Skipped: false, Log: log})
}

// GetIrrigationHistory godoc
// @Summary 获取灌溉历史
// @Description 获取灌溉执行历史记录
// @Tags 灌溉执行
// @Security ApiKeyAuth
// @Produce json
// @Param zone_id query int false "区域ID"
// @Param start_time query string false "开始时间 (RFC3339)"
// @Param end_time query string false "结束时间 (RFC3339)"
// @Param limit query int false "返回数量限制" default(100)
// @Success 200 {array} models.IrrigationLog
// @Router /api/irrigation/history [get]
func (c *IrrigationController) GetHistory(ctx *gin.Context) {
	var zoneID *uint
	if zoneIDStr := ctx.Query("zone_id"); zoneIDStr != "" {
		id, _ := strconv.ParseUint(zoneIDStr, 10, 32)
		idUint := uint(id)
		zoneID = &idUint
	}

	var startTime, endTime time.Time
	if startStr := ctx.Query("start_time"); startStr != "" {
		startTime, _ = time.Parse(time.RFC3339, startStr)
	}
	if endStr := ctx.Query("end_time"); endStr != "" {
		endTime, _ = time.Parse(time.RFC3339, endStr)
	}

	limit := 100
	if limitStr := ctx.Query("limit"); limitStr != "" {
		limit, _ = strconv.Atoi(limitStr)
	}

	logs, err := c.irrigationService.GetIrrigationHistory(zoneID, startTime, endTime, limit)
	if err != nil {
		response.InternalServerError(ctx, err.Error())
		return
	}

	response.Success(ctx, logs)
}

// GetWaterUsageStats godoc
// @Summary 获取用水量统计
// @Description 获取指定时间段的用水量统计
// @Tags 用水统计
// @Security ApiKeyAuth
// @Produce json
// @Param zone_id query int false "区域ID"
// @Param start_time query string false "开始时间 (RFC3339)"
// @Param end_time query string false "结束时间 (RFC3339)"
// @Success 200 {object} services.WaterUsageStats
// @Router /api/statistics/water-usage [get]
func (c *IrrigationController) GetWaterUsageStats(ctx *gin.Context) {
	var zoneID *uint
	if zoneIDStr := ctx.Query("zone_id"); zoneIDStr != "" {
		id, _ := strconv.ParseUint(zoneIDStr, 10, 32)
		idUint := uint(id)
		zoneID = &idUint
	}

	var startTime, endTime time.Time
	if startStr := ctx.Query("start_time"); startStr != "" {
		startTime, _ = time.Parse(time.RFC3339, startStr)
	}
	if endStr := ctx.Query("end_time"); endStr != "" {
		endTime, _ = time.Parse(time.RFC3339, endStr)
	}

	stats, err := c.irrigationService.GetWaterUsageStats(zoneID, startTime, endTime)
	if err != nil {
		response.InternalServerError(ctx, err.Error())
		return
	}

	response.Success(ctx, stats)
}

// GetZoneWaterUsage godoc
// @Summary 获取各区域用水量
// @Description 获取各区域的用水量分布
// @Tags 用水统计
// @Security ApiKeyAuth
// @Produce json
// @Param start_time query string false "开始时间 (RFC3339)"
// @Param end_time query string false "结束时间 (RFC3339)"
// @Success 200 {array} services.ZoneWaterUsage
// @Router /api/statistics/zone-usage [get]
func (c *IrrigationController) GetZoneWaterUsage(ctx *gin.Context) {
	var startTime, endTime time.Time
	if startStr := ctx.Query("start_time"); startStr != "" {
		startTime, _ = time.Parse(time.RFC3339, startStr)
	} else {
		startTime = time.Now().AddDate(0, 0, -7)
	}
	if endStr := ctx.Query("end_time"); endStr != "" {
		endTime, _ = time.Parse(time.RFC3339, endStr)
	} else {
		endTime = time.Now()
	}

	usage, err := c.irrigationService.GetZoneWaterUsage(startTime, endTime)
	if err != nil {
		response.InternalServerError(ctx, err.Error())
		return
	}

	response.Success(ctx, usage)
}
