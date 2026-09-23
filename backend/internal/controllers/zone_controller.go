package controllers

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"irrigation/internal/models"
	"irrigation/internal/services"
	"irrigation/pkg/response"
)

type ZoneController struct {
	zoneService       *services.ZoneService
	suspensionService *services.SuspensionService
	irrigationService *services.IrrigationService
}

func NewZoneController() *ZoneController {
	return &ZoneController{
		zoneService:       services.NewZoneService(),
		suspensionService: services.NewSuspensionService(),
		irrigationService: services.NewIrrigationService(),
	}
}

// ZoneDetail 区域详情，包含当前停灌状态和最近的跳过记录
type ZoneDetail struct {
	*models.IrrigationZone
	ActiveSuspension *models.IrrigationSuspension `json:"active_suspension"`
	RecentSkips      []models.IrrigationLog       `json:"recent_skips"`
}

// ListZones godoc
// @Summary 获取灌溉区域列表
// @Description 获取所有灌溉区域
// @Tags 灌溉区域
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {array} models.IrrigationZone
// @Router /api/zones [get]
func (c *ZoneController) List(ctx *gin.Context) {
	zones, err := c.zoneService.ListZones()
	if err != nil {
		response.InternalServerError(ctx, err.Error())
		return
	}
	response.Success(ctx, zones)
}

// GetZone godoc
// @Summary 获取灌溉区域详情
// @Description 根据ID获取灌溉区域详情，包含当前停灌状态（原因、预计结束时间）和最近的跳过记录
// @Tags 灌溉区域
// @Security ApiKeyAuth
// @Produce json
// @Param id path int true "区域ID"
// @Success 200 {object} controllers.ZoneDetail
// @Router /api/zones/{id} [get]
func (c *ZoneController) Get(ctx *gin.Context) {
	id, _ := strconv.ParseUint(ctx.Param("id"), 10, 32)
	zone, err := c.zoneService.GetZoneByID(uint(id))
	if err != nil {
		response.NotFound(ctx, "Zone not found")
		return
	}

	suspension, err := c.suspensionService.GetActiveSuspension(uint(id))
	if err != nil {
		response.InternalServerError(ctx, err.Error())
		return
	}

	skips, err := c.irrigationService.GetRecentSkips(uint(id), 5)
	if err != nil {
		response.InternalServerError(ctx, err.Error())
		return
	}

	response.Success(ctx, ZoneDetail{
		IrrigationZone:   zone,
		ActiveSuspension: suspension,
		RecentSkips:      skips,
	})
}

// CreateZone godoc
// @Summary 创建灌溉区域
// @Description 创建新的灌溉区域
// @Tags 灌溉区域
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body models.IrrigationZone true "区域信息"
// @Success 201 {object} models.IrrigationZone
// @Router /api/zones [post]
func (c *ZoneController) Create(ctx *gin.Context) {
	var zone models.IrrigationZone
	if err := ctx.ShouldBindJSON(&zone); err != nil {
		response.BadRequest(ctx, "Invalid request body")
		return
	}

	if err := c.zoneService.CreateZone(&zone); err != nil {
		response.InternalServerError(ctx, err.Error())
		return
	}

	response.Created(ctx, zone)
}

// UpdateZone godoc
// @Summary 更新灌溉区域
// @Description 更新灌溉区域信息
// @Tags 灌溉区域
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param id path int true "区域ID"
// @Param request body map[string]interface{} true "更新信息"
// @Success 200 {object} response.Response
// @Router /api/zones/{id} [put]
func (c *ZoneController) Update(ctx *gin.Context) {
	id, _ := strconv.ParseUint(ctx.Param("id"), 10, 32)
	
	var updates map[string]interface{}
	if err := ctx.ShouldBindJSON(&updates); err != nil {
		response.BadRequest(ctx, "Invalid request body")
		return
	}

	if err := c.zoneService.UpdateZone(uint(id), updates); err != nil {
		response.NotFound(ctx, err.Error())
		return
	}

	response.Success(ctx, nil)
}

// DeleteZone godoc
// @Summary 删除灌溉区域
// @Description 删除灌溉区域
// @Tags 灌溉区域
// @Security ApiKeyAuth
// @Produce json
// @Param id path int true "区域ID"
// @Success 200 {object} response.Response
// @Router /api/zones/{id} [delete]
func (c *ZoneController) Delete(ctx *gin.Context) {
	id, _ := strconv.ParseUint(ctx.Param("id"), 10, 32)
	
	if err := c.zoneService.DeleteZone(uint(id)); err != nil {
		response.NotFound(ctx, err.Error())
		return
	}

	response.Success(ctx, nil)
}
