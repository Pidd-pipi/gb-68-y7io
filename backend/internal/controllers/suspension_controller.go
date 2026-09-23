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
// @Summary 设置区域停灌
// @Description 设置区域临时停灌（区域检修、暴雨等场景），需填写原因和预计结束时间；重复设置只更新同一条生效中的记录
// @Tags 停灌管理
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param id path int true "区域ID"
// @Param request body object true "停灌信息" {"reason": "区域检修", "expected_end_at": "2026-09-25T18:00:00+08:00"}
// @Success 200 {object} models.IrrigationSuspension
// @Router /api/zones/{id}/suspension [put]
func (c *SuspensionController) Set(ctx *gin.Context) {
	id, _ := strconv.ParseUint(ctx.Param("id"), 10, 32)

	var req struct {
		Reason        string `json:"reason" binding:"required"`
		ExpectedEndAt string `json:"expected_end_at" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.BadRequest(ctx, "Invalid request body: reason and expected_end_at are required")
		return
	}

	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		response.BadRequest(ctx, "reason must not be empty")
		return
	}

	expectedEndAt, err := time.Parse(time.RFC3339, req.ExpectedEndAt)
	if err != nil {
		response.BadRequest(ctx, "expected_end_at must be in RFC3339 format")
		return
	}
	if !expectedEndAt.After(time.Now()) {
		response.BadRequest(ctx, "expected_end_at must be in the future")
		return
	}

	suspension, err := c.suspensionService.SetSuspension(uint(id), reason, expectedEndAt)
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

// GetCurrentSuspension godoc
// @Summary 获取区域当前停灌状态
// @Description 获取区域当前生效中的停灌记录（含原因和预计结束时间），无停灌时返回 null
// @Tags 停灌管理
// @Security ApiKeyAuth
// @Produce json
// @Param id path int true "区域ID"
// @Success 200 {object} models.IrrigationSuspension
// @Router /api/zones/{id}/suspension [get]
func (c *SuspensionController) GetCurrent(ctx *gin.Context) {
	id, _ := strconv.ParseUint(ctx.Param("id"), 10, 32)

	suspension, err := c.suspensionService.GetActiveSuspension(uint(id))
	if err != nil {
		response.InternalServerError(ctx, err.Error())
		return
	}

	response.Success(ctx, suspension)
}

// ListSuspensions godoc
// @Summary 获取区域停灌历史
// @Description 获取区域全部停灌记录（含已解除的），恢复后历史仍可查询
// @Tags 停灌管理
// @Security ApiKeyAuth
// @Produce json
// @Param id path int true "区域ID"
// @Param limit query int false "返回数量限制" default(50)
// @Success 200 {array} models.IrrigationSuspension
// @Router /api/zones/{id}/suspensions [get]
func (c *SuspensionController) History(ctx *gin.Context) {
	id, _ := strconv.ParseUint(ctx.Param("id"), 10, 32)

	limit := 50
	if limitStr := ctx.Query("limit"); limitStr != "" {
		limit, _ = strconv.Atoi(limitStr)
	}

	suspensions, err := c.suspensionService.ListSuspensions(uint(id), limit)
	if err != nil {
		response.InternalServerError(ctx, err.Error())
		return
	}

	response.Success(ctx, suspensions)
}

// LiftSuspension godoc
// @Summary 提前解除区域停灌
// @Description 手动提前解除区域当前生效中的停灌（不手动解除时，到达预计结束时间自动解除）
// @Tags 停灌管理
// @Security ApiKeyAuth
// @Produce json
// @Param id path int true "区域ID"
// @Success 200 {object} response.Response
// @Router /api/zones/{id}/suspension/lift [post]
func (c *SuspensionController) Lift(ctx *gin.Context) {
	id, _ := strconv.ParseUint(ctx.Param("id"), 10, 32)

	if err := c.suspensionService.LiftSuspension(uint(id)); err != nil {
		response.NotFound(ctx, err.Error())
		return
	}

	response.Success(ctx, nil)
}
