package monitoring_handler

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	monitoring_model "github.com/evolution-foundation/evolution-go/pkg/monitoring/model"
	monitoring_service "github.com/evolution-foundation/evolution-go/pkg/monitoring/service"
)

type MonitoringHandler interface {
	Prometheus(ctx *gin.Context)
	Dashboard(ctx *gin.Context)
	Events(ctx *gin.Context)
	GetAlerts(ctx *gin.Context)
	SetAlerts(ctx *gin.Context)
	TestAlert(ctx *gin.Context)
}

type monitoringHandler struct {
	service      monitoring_service.MonitoringService
	globalApiKey string
}

// Prometheus metrics
// @Summary Prometheus metrics
// @Description Metrics in the Prometheus text format. Authenticate with the global API key in the apikey header or as a Bearer token.
// @Tags Monitoring
// @Produce plain
// @Success 200 {string} string "Metrics"
// @Router /metrics [get]
func (h *monitoringHandler) Prometheus(ctx *gin.Context) {
	token := ctx.GetHeader("apikey")
	if token == "" {
		token = strings.TrimPrefix(ctx.GetHeader("Authorization"), "Bearer ")
	}
	if h.globalApiKey == "" || subtle.ConstantTimeCompare([]byte(token), []byte(h.globalApiKey)) != 1 {
		ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authorized"})
		return
	}
	ctx.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if err := h.service.WritePrometheus(ctx.Writer); err != nil {
		ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

// Get monitoring dashboard
// @Summary Get monitoring dashboard
// @Description Message volume, contacts, connection health and delivery failures of the last N days
// @Tags Monitoring
// @Produce json
// @Param days query int false "Period in days, including today (default 7, max 365)"
// @Param instanceId query string false "Only this instance"
// @Success 200 {object} gin.H "Dashboard"
// @Router /monitoring/dashboard [get]
func (h *monitoringHandler) Dashboard(ctx *gin.Context) {
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", "7"))
	d, err := h.service.Dashboard(days, ctx.Query("instanceId"))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": d})
}

// List monitoring events
// @Summary List monitoring events
// @Description Latest connection events and delivery failures
// @Tags Monitoring
// @Produce json
// @Param instanceId query string false "Only this instance"
// @Param limit query int false "Max events (default 100, max 500)"
// @Success 200 {object} gin.H "Events"
// @Router /monitoring/events [get]
func (h *monitoringHandler) Events(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "100"))
	events, err := h.service.Events(ctx.Query("instanceId"), limit)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": events})
}

// Get alert settings
// @Summary Get alert settings
// @Tags Monitoring
// @Produce json
// @Success 200 {object} gin.H "Alert settings"
// @Router /monitoring/alerts [get]
func (h *monitoringHandler) GetAlerts(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": h.service.GetAlertConfig()})
}

// Set alert settings
// @Summary Set alert settings
// @Description Alerts for disconnected/logged out/banned instances and delivery failures, sent by WhatsApp and/or webhook
// @Tags Monitoring
// @Accept json
// @Produce json
// @Success 200 {object} gin.H "Alert settings"
// @Router /monitoring/alerts [put]
func (h *monitoringHandler) SetAlerts(ctx *gin.Context) {
	var cfg monitoring_model.AlertConfig
	if err := ctx.ShouldBindJSON(&cfg); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.service.SaveAlertConfig(&cfg); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": h.service.GetAlertConfig()})
}

// Send a test alert
// @Summary Send a test alert
// @Description Sends a test message to every configured alert channel
// @Tags Monitoring
// @Produce json
// @Success 200 {object} gin.H "Sent"
// @Router /monitoring/alerts/test [post]
func (h *monitoringHandler) TestAlert(ctx *gin.Context) {
	if err := h.service.SendTestAlert(); err != nil {
		ctx.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

func NewMonitoringHandler(service monitoring_service.MonitoringService, globalApiKey string) MonitoringHandler {
	return &monitoringHandler{service: service, globalApiKey: globalApiKey}
}
