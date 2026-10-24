package typebot_handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	instance_model "github.com/evolution-foundation/evolution-go/pkg/instance/model"
	typebot_model "github.com/evolution-foundation/evolution-go/pkg/typebot/model"
	typebot_service "github.com/evolution-foundation/evolution-go/pkg/typebot/service"
)

type TypebotHandler interface {
	GetConfig(ctx *gin.Context)
	SetConfig(ctx *gin.Context)
	DeleteConfig(ctx *gin.Context)
	ListSessions(ctx *gin.Context)
	CloseSessions(ctx *gin.Context)
}

type typebotHandler struct {
	typebotService typebot_service.TypebotService
}

type configResponse struct {
	*typebot_model.TypebotConfig
	Configured bool `json:"configured"`
}

func response(instanceID string, cfg *typebot_model.TypebotConfig) configResponse {
	configured := cfg != nil
	if cfg == nil {
		// Defaults shown by the manager for a new integration.
		cfg = &typebot_model.TypebotConfig{
			InstanceID:      instanceID,
			TriggerType:     typebot_model.TriggerKeyword,
			TriggerOperator: typebot_model.OperatorEquals,
			ExpireMinutes:   60,
			KeywordFinish:   "#sair",
			DelayMessageMs:  1000,
			UnknownMessage:  typebot_service.DefaultUnknownMessage,
			StopBotFromMe:   true,
			HandoffChatwoot: true,
		}
	}
	return configResponse{TypebotConfig: cfg, Configured: configured}
}

func instanceFrom(ctx *gin.Context) (*instance_model.Instance, bool) {
	instance, ok := ctx.MustGet("instance").(*instance_model.Instance)
	if !ok {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
	}
	return instance, ok
}

// Get Typebot integration settings
// @Summary Get Typebot integration settings
// @Description Get the Typebot integration settings of the instance
// @Tags Typebot
// @Produce json
// @Success 200 {object} gin.H "Typebot settings"
// @Router /typebot/config [get]
func (h *typebotHandler) GetConfig(ctx *gin.Context) {
	instance, ok := instanceFrom(ctx)
	if !ok {
		return
	}
	cfg, err := h.typebotService.GetConfig(instance.Id)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": response(instance.Id, cfg)})
}

// Set Typebot integration settings
// @Summary Set Typebot integration settings
// @Description Save the Typebot settings (the bot must be published on the viewer)
// @Tags Typebot
// @Accept json
// @Produce json
// @Param config body typebot_service.ConfigInput true "Typebot settings"
// @Success 200 {object} gin.H "Typebot settings"
// @Failure 400 {object} gin.H "Error on validation"
// @Router /typebot/config [put]
func (h *typebotHandler) SetConfig(ctx *gin.Context) {
	instance, ok := instanceFrom(ctx)
	if !ok {
		return
	}
	var input typebot_service.ConfigInput
	if err := ctx.ShouldBindJSON(&input); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cfg, err := h.typebotService.SaveConfig(ctx.Request.Context(), instance.Id, input)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": response(instance.Id, cfg)})
}

// Delete Typebot integration settings
// @Summary Delete Typebot integration settings
// @Description Remove the Typebot integration and its sessions
// @Tags Typebot
// @Produce json
// @Success 200 {object} gin.H "success"
// @Router /typebot/config [delete]
func (h *typebotHandler) DeleteConfig(ctx *gin.Context) {
	instance, ok := instanceFrom(ctx)
	if !ok {
		return
	}
	if err := h.typebotService.DeleteConfig(instance.Id); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

// List active Typebot sessions
// @Summary List Typebot sessions
// @Description List the contacts currently talking to (or paused in) the bot
// @Tags Typebot
// @Produce json
// @Success 200 {object} gin.H "sessions"
// @Router /typebot/sessions [get]
func (h *typebotHandler) ListSessions(ctx *gin.Context) {
	instance, ok := instanceFrom(ctx)
	if !ok {
		return
	}
	sessions, err := h.typebotService.ListSessions(instance.Id)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if sessions == nil {
		sessions = []typebot_model.TypebotSession{}
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": sessions})
}

// Close all Typebot sessions
// @Summary Close Typebot sessions
// @Description End every bot session of the instance (contacts restart on the next trigger)
// @Tags Typebot
// @Produce json
// @Success 200 {object} gin.H "success"
// @Router /typebot/sessions [delete]
func (h *typebotHandler) CloseSessions(ctx *gin.Context) {
	instance, ok := instanceFrom(ctx)
	if !ok {
		return
	}
	if err := h.typebotService.CloseSessions(instance.Id); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

func NewTypebotHandler(typebotService typebot_service.TypebotService) TypebotHandler {
	return &typebotHandler{typebotService: typebotService}
}
