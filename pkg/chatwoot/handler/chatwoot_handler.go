package chatwoot_handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
	chatwoot_service "github.com/evolution-foundation/evolution-go/pkg/chatwoot/service"
	instance_model "github.com/evolution-foundation/evolution-go/pkg/instance/model"
)

type ChatwootHandler interface {
	GetConfig(ctx *gin.Context)
	SetConfig(ctx *gin.Context)
	DeleteConfig(ctx *gin.Context)
	ImportContacts(ctx *gin.Context)
	Webhook(ctx *gin.Context)
}

type chatwootHandler struct {
	chatwootService chatwoot_service.ChatwootService
}

type configResponse struct {
	*chatwoot_model.ChatwootConfig
	WebhookURL      string `json:"webhookUrl"`
	ImportAvailable bool   `json:"importAvailable"`
	Configured      bool   `json:"configured"`
}

func (h *chatwootHandler) response(instanceID string, cfg *chatwoot_model.ChatwootConfig) configResponse {
	configured := cfg != nil
	if cfg == nil {
		// Defaults shown by the manager for a new integration.
		cfg = &chatwoot_model.ChatwootConfig{
			InstanceID:          instanceID,
			AutoCreateInbox:     true,
			SignMessages:        true,
			SignDelimiter:       `\n`,
			ReopenConversation:  true,
			MergeBrazilContacts: true,
			DaysLimitImport:     3,
		}
	}
	return configResponse{
		ChatwootConfig:  cfg,
		WebhookURL:      h.chatwootService.WebhookURL(cfg),
		ImportAvailable: h.chatwootService.ImportAvailable(),
		Configured:      configured,
	}
}

func instanceFrom(ctx *gin.Context) (*instance_model.Instance, bool) {
	instance, ok := ctx.MustGet("instance").(*instance_model.Instance)
	if !ok {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
	}
	return instance, ok
}

// Get Chatwoot integration settings
// @Summary Get Chatwoot integration settings
// @Description Get the Chatwoot integration settings of the instance
// @Tags Chatwoot
// @Produce json
// @Success 200 {object} gin.H "Chatwoot settings"
// @Router /chatwoot/config [get]
func (h *chatwootHandler) GetConfig(ctx *gin.Context) {
	instance, ok := instanceFrom(ctx)
	if !ok {
		return
	}
	cfg, err := h.chatwootService.GetConfig(instance.Id)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": h.response(instance.Id, cfg)})
}

// Set Chatwoot integration settings
// @Summary Set Chatwoot integration settings
// @Description Save the Chatwoot settings, creating the API inbox and its webhook when needed
// @Tags Chatwoot
// @Accept json
// @Produce json
// @Param config body chatwoot_service.ConfigInput true "Chatwoot settings"
// @Success 200 {object} gin.H "Chatwoot settings"
// @Failure 400 {object} gin.H "Error on validation"
// @Router /chatwoot/config [put]
func (h *chatwootHandler) SetConfig(ctx *gin.Context) {
	instance, ok := instanceFrom(ctx)
	if !ok {
		return
	}
	var input chatwoot_service.ConfigInput
	if err := ctx.ShouldBindJSON(&input); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cfg, err := h.chatwootService.SaveConfig(ctx.Request.Context(), instance.Id, input)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": h.response(instance.Id, cfg)})
}

// Delete Chatwoot integration settings
// @Summary Delete Chatwoot integration settings
// @Description Remove the Chatwoot integration of the instance (the Chatwoot inbox is kept)
// @Tags Chatwoot
// @Produce json
// @Success 200 {object} gin.H "success"
// @Router /chatwoot/config [delete]
func (h *chatwootHandler) DeleteConfig(ctx *gin.Context) {
	instance, ok := instanceFrom(ctx)
	if !ok {
		return
	}
	if err := h.chatwootService.DeleteConfig(instance.Id); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

// Import WhatsApp contacts into Chatwoot
// @Summary Import contacts into Chatwoot
// @Description Create Chatwoot contacts for the WhatsApp address book (runs in background)
// @Tags Chatwoot
// @Produce json
// @Success 202 {object} gin.H "started"
// @Router /chatwoot/import-contacts [post]
func (h *chatwootHandler) ImportContacts(ctx *gin.Context) {
	instance, ok := instanceFrom(ctx)
	if !ok {
		return
	}
	cfg, err := h.chatwootService.GetConfig(instance.Id)
	if err != nil || cfg == nil || !cfg.Enabled {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "integração com o Chatwoot não está ativa"})
		return
	}
	go func(id string) {
		_ = h.chatwootService.ImportContacts(id)
	}(instance.Id)
	ctx.JSON(http.StatusAccepted, gin.H{"message": "started"})
}

// Webhook receives events from the Chatwoot API inbox. Public route, protected
// by the per-instance secret embedded in the URL.
func (h *chatwootHandler) Webhook(ctx *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, 5<<20))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	err = h.chatwootService.HandleWebhook(ctx.Param("instanceId"), ctx.Param("secret"), body)
	if errors.Is(err, chatwoot_service.ErrUnauthorized) {
		ctx.JSON(http.StatusUnauthorized, gin.H{"error": "not authorized"})
		return
	}
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

func NewChatwootHandler(chatwootService chatwoot_service.ChatwootService) ChatwootHandler {
	return &chatwootHandler{chatwootService: chatwootService}
}
