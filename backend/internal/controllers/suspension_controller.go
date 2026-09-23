package controllers

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"irrigation/internal/services"
	"irrigation/pkg/response"
)

type SuspensionController struct {
	suspensionService *services.SuspensionService
}

func NewSuspensionController() *SuspensionController {
	return &SuspensionController{
		suspensionService: services.NewSuspensionService(),
	}
}

// SetSuspension godoc
// @Summary 设置区域临时停灌
// @Description 区域检修或暴雨时设置临时停灌，需填写原因和预计结束时间；重复设置只更新同一条生效中的记录
// @Tags 区域停灌
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param id path int true "区域ID"
// @Param request body object true "停灌信息" example({"reason":"区域管道检修","expected_end_time":"2026-09-25T18:00:00+08:00"})
// @Success 200 {object} models.ZoneSuspension
// @Router /api/zones/{id}/suspension [post]
func (c *SuspensionController) Set(ctx *gin.Context) {
	zoneID, _ := strconv.ParseUint(ctx.Param("id"), 10, 32)

	var req struct {
		Reason          string `json:"reason" binding:"required"`
		ExpectedEndTime string `json:"expected_end_time" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.BadRequest(ctx, "Invalid request body: reason and expected_end_time are required")
		return
	}

	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		response.BadRequest(ctx, "reason must not be empty")
		return
	}

	expectedEndTime, err := time.Parse(time.RFC3339, req.ExpectedEndTime)
	if err != nil {
		response.BadRequest(ctx, "expected_end_time must be in RFC3339 format")
		return
	}
	if !expectedEndTime.After(time.Now()) {
		response.BadRequest(ctx, "expected_end_time must be in the future")
		return
	}

	suspension, err := c.suspensionService.SetSuspension(uint(zoneID), reason, expectedEndTime)
	if err != nil {
		if err.Error() == "zone not found" {
			response.NotFound(ctx, err.Error())
			return
		}
		response.InternalServerError(ctx, err.Error())
		return
	}

	response.Success(ctx, suspension)
}

// ResumeSuspension godoc
// @Summary 解除区域停灌
// @Description 手动解除区域当前生效的停灌，历史记录保留可查询
// @Tags 区域停灌
// @Security ApiKeyAuth
// @Produce json
// @Param id path int true "区域ID"
// @Success 200 {object} response.Response
// @Router /api/zones/{id}/suspension/resume [post]
func (c *SuspensionController) Resume(ctx *gin.Context) {
	zoneID, _ := strconv.ParseUint(ctx.Param("id"), 10, 32)

	if err := c.suspensionService.ResumeSuspension(uint(zoneID)); err != nil {
		response.NotFound(ctx, err.Error())
		return
	}

	response.Success(ctx, nil)
}

// GetSuspension godoc
// @Summary 获取区域当前停灌状态
// @Description 获取区域当前生效中的停灌记录（含原因和预计结束时间），无停灌时返回 null
// @Tags 区域停灌
// @Security ApiKeyAuth
// @Produce json
// @Param id path int true "区域ID"
// @Success 200 {object} models.ZoneSuspension
// @Router /api/zones/{id}/suspension [get]
func (c *SuspensionController) Get(ctx *gin.Context) {
	zoneID, _ := strconv.ParseUint(ctx.Param("id"), 10, 32)

	suspension, err := c.suspensionService.GetActiveSuspension(uint(zoneID))
	if err != nil {
		response.InternalServerError(ctx, err.Error())
		return
	}

	response.Success(ctx, suspension)
}

// GetSuspensionHistory godoc
// @Summary 获取区域停灌历史
// @Description 获取区域全部停灌记录（含已解除和已到期的），恢复后历史仍可查询
// @Tags 区域停灌
// @Security ApiKeyAuth
// @Produce json
// @Param id path int true "区域ID"
// @Param limit query int false "返回数量限制" default(50)
// @Success 200 {array} models.ZoneSuspension
// @Router /api/zones/{id}/suspension/history [get]
func (c *SuspensionController) GetHistory(ctx *gin.Context) {
	zoneID, _ := strconv.ParseUint(ctx.Param("id"), 10, 32)

	limit := 50
	if limitStr := ctx.Query("limit"); limitStr != "" {
		limit, _ = strconv.Atoi(limitStr)
	}

	suspensions, err := c.suspensionService.GetSuspensionHistory(uint(zoneID), limit)
	if err != nil {
		response.InternalServerError(ctx, err.Error())
		return
	}

	response.Success(ctx, suspensions)
}

// GetSuspensionSkips godoc
// @Summary 获取区域灌溉跳过记录
// @Description 获取停灌期间被跳过的灌溉记录（定时计划、条件触发、手动浇水）
// @Tags 区域停灌
// @Security ApiKeyAuth
// @Produce json
// @Param id path int true "区域ID"
// @Param limit query int false "返回数量限制" default(20)
// @Success 200 {array} models.IrrigationSkipLog
// @Router /api/zones/{id}/suspension/skips [get]
func (c *SuspensionController) GetSkips(ctx *gin.Context) {
	zoneID, _ := strconv.ParseUint(ctx.Param("id"), 10, 32)

	limit := 20
	if limitStr := ctx.Query("limit"); limitStr != "" {
		limit, _ = strconv.Atoi(limitStr)
	}

	skips, err := c.suspensionService.ListRecentSkips(uint(zoneID), limit)
	if err != nil {
		response.InternalServerError(ctx, err.Error())
		return
	}

	response.Success(ctx, skips)
}
