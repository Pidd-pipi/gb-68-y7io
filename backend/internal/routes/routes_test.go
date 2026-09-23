package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"irrigation/internal/config"
	"irrigation/internal/middleware"
	"irrigation/pkg/database"
)

var (
	testEngine *gin.Engine
	testToken  string
)

type apiResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func setupE2E(t *testing.T) {
	t.Helper()

	runtimePath := filepath.Join(os.TempDir(), "embedded-postgres-routes")
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Port(55435).
		Database("irrigation_e2e").
		Username("postgres").
		Password("postgres").
		RuntimePath(runtimePath))
	if err := pg.Start(); err != nil {
		t.Fatalf("failed to start embedded postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = pg.Stop()
	})

	dsn := "host=localhost port=55435 user=postgres password=postgres dbname=irrigation_e2e sslmode=disable"
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

	config.AppSettings = &config.Config{
		JWT: config.JWTConfig{Secret: "e2e-secret", ExpireHours: 24},
	}

	gin.SetMode(gin.TestMode)
	testEngine = gin.New()
	SetupRoutes(testEngine)

	token, err := middleware.GenerateToken(1, "admin")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}
	testToken = token
}

func doRequest(t *testing.T, method, path string, body interface{}) (int, apiResponse) {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)

	recorder := httptest.NewRecorder()
	testEngine.ServeHTTP(recorder, req)

	var resp apiResponse
	_ = json.Unmarshal(recorder.Body.Bytes(), &resp)
	return recorder.Code, resp
}

func TestSuspensionEndToEnd(t *testing.T) {
	setupE2E(t)

	// 创建区域
	code, resp := doRequest(t, http.MethodPost, "/api/zones", map[string]interface{}{
		"name":        "月季园",
		"description": "入口花坛区域",
	})
	if code != http.StatusCreated {
		t.Fatalf("create zone failed: code=%d msg=%s", code, resp.Message)
	}
	var zone struct {
		ID uint `json:"id"`
	}
	if err := json.Unmarshal(resp.Data, &zone); err != nil || zone.ID == 0 {
		t.Fatalf("invalid zone response: %s", resp.Data)
	}
	zonePath := fmt.Sprintf("/api/zones/%d", zone.ID)

	// 设置停灌：缺少原因应 400
	code, _ = doRequest(t, http.MethodPost, zonePath+"/suspension", map[string]interface{}{
		"expected_end_time": time.Now().Add(2 * time.Hour).Format(time.RFC3339),
	})
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing reason, got %d", code)
	}

	// 正常设置停灌
	end1 := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	code, resp = doRequest(t, http.MethodPost, zonePath+"/suspension", map[string]interface{}{
		"reason":            "区域管道检修",
		"expected_end_time": end1.Format(time.RFC3339),
	})
	if code != http.StatusOK {
		t.Fatalf("set suspension failed: code=%d msg=%s", code, resp.Message)
	}
	var s1 struct {
		ID     uint   `json:"id"`
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(resp.Data, &s1); err != nil {
		t.Fatalf("invalid suspension response: %s", resp.Data)
	}
	if s1.Status != "active" || s1.Reason != "区域管道检修" {
		t.Fatalf("unexpected suspension: %s", resp.Data)
	}

	// 重复设置只更新同一条记录
	code, resp = doRequest(t, http.MethodPost, zonePath+"/suspension", map[string]interface{}{
		"reason":            "暴雨红色预警",
		"expected_end_time": time.Now().Add(6 * time.Hour).Format(time.RFC3339),
	})
	if code != http.StatusOK {
		t.Fatalf("update suspension failed: code=%d", code)
	}
	var s2 struct {
		ID     uint   `json:"id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(resp.Data, &s2); err != nil || s2.ID != s1.ID {
		t.Fatalf("expected same suspension record updated: %s", resp.Data)
	}
	if s2.Reason != "暴雨红色预警" {
		t.Fatalf("reason not updated: %s", resp.Data)
	}

	// 停灌期间手动浇水被跳过并记录
	code, resp = doRequest(t, http.MethodPost, "/api/irrigation/manual", map[string]interface{}{
		"zone_id": zone.ID,
	})
	if code != http.StatusConflict {
		t.Fatalf("expected 409 for manual irrigation during suspension, got %d", code)
	}

	// 区域详情包含停灌状态、原因和最近跳过记录
	code, resp = doRequest(t, http.MethodGet, zonePath, nil)
	if code != http.StatusOK {
		t.Fatalf("get zone detail failed: code=%d", code)
	}
	var detail struct {
		ID         uint `json:"id"`
		Suspension *struct {
			Reason string `json:"reason"`
			Status string `json:"status"`
		} `json:"suspension"`
		RecentSkips []struct {
			TriggerType string `json:"trigger_type"`
			Reason      string `json:"reason"`
		} `json:"recent_skips"`
	}
	if err := json.Unmarshal(resp.Data, &detail); err != nil {
		t.Fatalf("invalid zone detail: %s", resp.Data)
	}
	if detail.Suspension == nil || detail.Suspension.Status != "active" || detail.Suspension.Reason != "暴雨红色预警" {
		t.Fatalf("zone detail missing suspension: %s", resp.Data)
	}
	if len(detail.RecentSkips) != 1 || detail.RecentSkips[0].TriggerType != "manual" {
		t.Fatalf("zone detail missing skip record: %s", resp.Data)
	}

	// 跳过记录接口
	code, resp = doRequest(t, http.MethodGet, zonePath+"/suspension/skips", nil)
	if code != http.StatusOK {
		t.Fatalf("get skips failed: code=%d", code)
	}
	var skips []struct {
		TriggerType string `json:"trigger_type"`
	}
	if err := json.Unmarshal(resp.Data, &skips); err != nil || len(skips) != 1 {
		t.Fatalf("expected 1 skip record: %s", resp.Data)
	}

	// 紧急启动：缺少书面原因应 400
	code, _ = doRequest(t, http.MethodPost, "/api/irrigation/emergency", map[string]interface{}{
		"zone_id": zone.ID,
	})
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for emergency without reason, got %d", code)
	}

	// 紧急启动：填写书面原因后立即执行，不受停灌限制
	code, resp = doRequest(t, http.MethodPost, "/api/irrigation/emergency", map[string]interface{}{
		"zone_id": zone.ID,
		"reason":  "设备漏水，需立即冲洗检修",
	})
	if code != http.StatusOK {
		t.Fatalf("emergency irrigation failed: code=%d msg=%s", code, resp.Message)
	}
	var emergencyLog struct {
		TriggerType string  `json:"trigger_type"`
		Remark      *string `json:"remark"`
	}
	if err := json.Unmarshal(resp.Data, &emergencyLog); err != nil {
		t.Fatalf("invalid emergency log: %s", resp.Data)
	}
	if emergencyLog.TriggerType != "emergency" || emergencyLog.Remark == nil {
		t.Fatalf("unexpected emergency log: %s", resp.Data)
	}

	// 紧急启动后原停灌状态仍生效（按预计时间解除）
	code, resp = doRequest(t, http.MethodGet, zonePath+"/suspension", nil)
	if code != http.StatusOK {
		t.Fatalf("get suspension failed: code=%d", code)
	}
	var current struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(resp.Data, &current); err != nil || current.Status != "active" {
		t.Fatalf("suspension should remain active after emergency: %s", resp.Data)
	}

	// 解除停灌
	code, _ = doRequest(t, http.MethodPost, zonePath+"/suspension/resume", nil)
	if code != http.StatusOK {
		t.Fatalf("resume suspension failed: code=%d", code)
	}

	// 解除后当前状态为空
	code, resp = doRequest(t, http.MethodGet, zonePath+"/suspension", nil)
	if code != http.StatusOK {
		t.Fatalf("get suspension after resume failed: code=%d", code)
	}
	if string(resp.Data) != "null" {
		t.Fatalf("expected null suspension after resume, got %s", resp.Data)
	}

	// 恢复后历史仍可查询
	code, resp = doRequest(t, http.MethodGet, zonePath+"/suspension/history", nil)
	if code != http.StatusOK {
		t.Fatalf("get suspension history failed: code=%d", code)
	}
	var history []struct {
		Status  string  `json:"status"`
		EndedAt *string `json:"ended_at"`
	}
	if err := json.Unmarshal(resp.Data, &history); err != nil || len(history) != 1 {
		t.Fatalf("expected 1 history record: %s", resp.Data)
	}
	if history[0].Status != "ended" || history[0].EndedAt == nil {
		t.Fatalf("expected ended history record: %s", resp.Data)
	}

	// 恢复后手动浇水恢复正常
	code, _ = doRequest(t, http.MethodPost, "/api/irrigation/manual", map[string]interface{}{
		"zone_id": zone.ID,
	})
	if code != http.StatusOK {
		t.Fatalf("manual irrigation after resume failed: code=%d", code)
	}
}
