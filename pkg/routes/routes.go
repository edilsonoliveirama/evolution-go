package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	cloudapi "github.com/evolution-foundation/evolution-go/pkg/cloudapi"
	_ "github.com/evolution-foundation/evolution-go/docs"
	call_handler "github.com/evolution-foundation/evolution-go/pkg/call/handler"
	chat_handler "github.com/evolution-foundation/evolution-go/pkg/chat/handler"
	chatwoot_handler "github.com/evolution-foundation/evolution-go/pkg/chatwoot/handler"
	community_handler "github.com/evolution-foundation/evolution-go/pkg/community/handler"
	group_handler "github.com/evolution-foundation/evolution-go/pkg/group/handler"
	instance_handler "github.com/evolution-foundation/evolution-go/pkg/instance/handler"
	label_handler "github.com/evolution-foundation/evolution-go/pkg/label/handler"
	message_handler "github.com/evolution-foundation/evolution-go/pkg/message/handler"
	auth_middleware "github.com/evolution-foundation/evolution-go/pkg/middleware"
	monitoring_handler "github.com/evolution-foundation/evolution-go/pkg/monitoring/handler"
	newsletter_handler "github.com/evolution-foundation/evolution-go/pkg/newsletter/handler"
	poll_handler "github.com/evolution-foundation/evolution-go/pkg/poll/handler"
	send_handler "github.com/evolution-foundation/evolution-go/pkg/sendMessage/handler"
	server_handler "github.com/evolution-foundation/evolution-go/pkg/server/handler"
	typebot_handler "github.com/evolution-foundation/evolution-go/pkg/typebot/handler"
	user_handler "github.com/evolution-foundation/evolution-go/pkg/user/handler"
)

type Routes struct {
	authMiddleware          auth_middleware.Middleware
	jidValidationMiddleware *auth_middleware.JIDValidationMiddleware
	instanceHandler         instance_handler.InstanceHandler
	userHandler             user_handler.UserHandler
	sendHandler             send_handler.SendHandler
	messageHandler          message_handler.MessageHandler
	chatHandler             chat_handler.ChatHandler
	groupHandler            group_handler.GroupHandler
	callHandler             call_handler.CallHandler
	communityHandler        community_handler.CommunityHandler
	labelHandler            label_handler.LabelHandler
	newsletterHandler       newsletter_handler.NewsletterHandler
	pollHandler             *poll_handler.PollHandler
	serverHandler           server_handler.ServerHandler
	chatwootHandler         chatwoot_handler.ChatwootHandler
	typebotHandler          typebot_handler.TypebotHandler
	monitoringHandler       monitoring_handler.MonitoringHandler
	cloudApiHandler        *cloudapi.CloudApiHandler
}

func (r *Routes) AssignRoutes(eng *gin.Engine) {
	// Meta WhatsApp Cloud API webhook routes — PUBLIC (called by Meta servers, no auth)
	metaGroup := eng.Group("/meta")
	metaGroup.GET("/webhook/:instanceId", gin.WrapF(func(w http.ResponseWriter, req *http.Request) {
		r.cloudApiHandler.HandleWebhookGET(w, req)
	}))
	metaGroup.POST("/webhook/:instanceId", gin.WrapF(func(w http.ResponseWriter, req *http.Request) {
		r.cloudApiHandler.HandleWebhookPOST(w, req)
	}))
	// Configuração do CORS
	eng.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, Accept, Cache-Control, X-Requested-With, apikey, ApiKey")
		c.Writer.Header().Set("Access-Control-Expose-Headers", "Content-Length")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(200)
			return
		}

		c.Next()
	})

	eng.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	eng.GET("/favicon.ico", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	// Rotas para o gerenciador React (sem autenticação)
	eng.Static("/assets", "./manager/dist/assets")

	// Ajuste nas rotas do manager para suportar client-side routing do React
	eng.GET("/manager/*any", func(c *gin.Context) {
		c.File("manager/dist/index.html")
	})

	eng.GET("/manager", func(c *gin.Context) {
		c.File("manager/dist/index.html")
	})

	eng.GET("/server/ok", r.serverHandler.ServerOk)

	routes := eng.Group("/instance")
	{
		routes.Use(r.authMiddleware.AuthAdmin)
		{
			routes.POST("/create", r.instanceHandler.Create)
			routes.GET("/all", r.instanceHandler.All)
			routes.GET("/stats", r.instanceHandler.Stats)
			routes.GET("/info/:instanceId", r.instanceHandler.Info)
			routes.DELETE("/delete/:instanceId", r.instanceHandler.Delete)
			routes.POST("/proxy/:instanceId", r.instanceHandler.SetProxy)
			routes.DELETE("/proxy/:instanceId", r.instanceHandler.DeleteProxy)
			routes.POST("/forcereconnect/:instanceId", r.instanceHandler.ForceReconnect)
			routes.GET("/logs/:instanceId", r.instanceHandler.GetLogs)
		}
	}

	routes = eng.Group("/instance")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/connect", r.instanceHandler.Connect)
			routes.GET("/status", r.instanceHandler.Status)
			routes.GET("/qr", r.instanceHandler.Qr)
			routes.POST("/pair", r.jidValidationMiddleware.ValidateNumberField(), r.instanceHandler.Pair)
			routes.POST("/disconnect", r.instanceHandler.Disconnect)
			routes.POST("/reconnect", r.instanceHandler.Reconnect)
			routes.DELETE("/logout", r.instanceHandler.Logout)
			routes.GET("/:instanceId/advanced-settings", r.instanceHandler.GetAdvancedSettings)
			routes.PUT("/:instanceId/advanced-settings", r.instanceHandler.UpdateAdvancedSettings)
		}
	}

	routes = eng.Group("/send")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/text", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendText)
			routes.POST("/link", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendLink)
			routes.POST("/media", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendMedia)
			routes.POST("/poll", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendPoll)
			routes.POST("/sticker", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendSticker)
			routes.POST("/location", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendLocation)
			routes.POST("/contact", r.jidValidationMiddleware.ValidateContactFields(), r.sendHandler.SendContact) // TODO: send multiple contacts
			routes.POST("/button", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendButton)
			routes.POST("/list", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendList)
			routes.POST("/carousel", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendCarousel)
			routes.POST("/template", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendTemplate)
			routes.POST("/status/text", r.sendHandler.SendStatusText)
			routes.POST("/status/media", r.sendHandler.SendStatusMedia)
		}
	}
	routes = eng.Group("/user")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/info", r.jidValidationMiddleware.ValidateNumberField(), r.userHandler.GetUser)
			routes.POST("/check", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.userHandler.CheckUser)
			routes.POST("/avatar", r.jidValidationMiddleware.ValidateNumberField(), r.userHandler.GetAvatar)
			routes.GET("/contacts", r.userHandler.GetContacts)
			routes.GET("/privacy", r.userHandler.GetPrivacy)
			routes.POST("/privacy", r.userHandler.SetPrivacy)
			routes.POST("/block", r.jidValidationMiddleware.ValidateNumberField(), r.userHandler.BlockContact)
			routes.POST("/unblock", r.jidValidationMiddleware.ValidateNumberField(), r.userHandler.UnblockContact)
			routes.GET("/blocklist", r.userHandler.GetBlockList)
			routes.POST("/profilePicture", r.userHandler.SetProfilePicture)
			routes.POST("/profileName", r.userHandler.SetProfileName)
			routes.POST("/profileStatus", r.userHandler.SetProfileStatus)
		}
	}
	routes = eng.Group("/message")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/react", r.jidValidationMiddleware.ValidateJIDFields("number"), r.messageHandler.React)
			routes.POST("/presence", r.jidValidationMiddleware.ValidateNumberField(), r.messageHandler.ChatPresence)
			routes.POST("/markread", r.jidValidationMiddleware.ValidateNumberField(), r.messageHandler.MarkRead)
			routes.POST("/markplayed", r.jidValidationMiddleware.ValidateNumberField(), r.messageHandler.MarkPlayed)
			routes.POST("/downloadmedia", r.messageHandler.DownloadMedia)
			routes.POST("/status", r.messageHandler.GetMessageStatus)
			routes.POST("/delete", r.jidValidationMiddleware.ValidateNumberField(), r.messageHandler.DeleteMessageEveryone)
			routes.POST("/edit", r.jidValidationMiddleware.ValidateNumberField(), r.messageHandler.EditMessage) // TODO: edit MediaMessage too
		}
	}
	routes = eng.Group("/chat")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/pin", r.jidValidationMiddleware.ValidateNumberField(), r.chatHandler.ChatPin)             // TODO: not working
			routes.POST("/unpin", r.jidValidationMiddleware.ValidateNumberField(), r.chatHandler.ChatUnpin)         // TODO: not working
			routes.POST("/archive", r.jidValidationMiddleware.ValidateNumberField(), r.chatHandler.ChatArchive)     // TODO: not working
			routes.POST("/unarchive", r.jidValidationMiddleware.ValidateNumberField(), r.chatHandler.ChatUnarchive) // TODO: not working
			routes.POST("/mute", r.jidValidationMiddleware.ValidateNumberField(), r.chatHandler.ChatMute)           // TODO: not working
			routes.POST("/unmute", r.jidValidationMiddleware.ValidateNumberField(), r.chatHandler.ChatUnmute)       // TODO: not working
			routes.POST("/history-sync", r.chatHandler.HistorySyncRequest)
		}
	}
	routes = eng.Group("/group")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.GET("/list", r.groupHandler.ListGroups)
			routes.POST("/info", r.jidValidationMiddleware.ValidateNumberField(), r.groupHandler.GetGroupInfo)
			routes.POST("/invitelink", r.jidValidationMiddleware.ValidateNumberField(), r.groupHandler.GetGroupInviteLink)
			routes.POST("/photo", r.jidValidationMiddleware.ValidateNumberField(), r.groupHandler.SetGroupPhoto)
			routes.POST("/name", r.jidValidationMiddleware.ValidateNumberField(), r.groupHandler.SetGroupName)
			routes.POST("/description", r.jidValidationMiddleware.ValidateNumberField(), r.groupHandler.SetGroupDescription)
			routes.POST("/create", r.jidValidationMiddleware.ValidateMultipleNumbers("participants"), r.groupHandler.CreateGroup)
			routes.POST("/participant", r.jidValidationMiddleware.ValidateJIDFields("number", "participants"), r.groupHandler.UpdateParticipant)
			routes.GET("/myall", r.groupHandler.GetMyGroups) // TODO: not working
			routes.POST("/join", r.groupHandler.JoinGroupLink)
			routes.POST("/leave", r.jidValidationMiddleware.ValidateNumberField(), r.groupHandler.LeaveGroup)
			routes.POST("/settings", r.jidValidationMiddleware.ValidateNumberField(), r.groupHandler.UpdateGroupSettings)
		}
	}
	routes = eng.Group("/call")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/reject", r.jidValidationMiddleware.ValidateNumberField(), r.callHandler.RejectCall)
		}
	}
	routes = eng.Group("/community")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/create", r.communityHandler.CreateCommunity)
			routes.POST("/add", r.jidValidationMiddleware.ValidateJIDFields("number", "communityId"), r.communityHandler.CommunityAdd)
			routes.POST("/remove", r.jidValidationMiddleware.ValidateJIDFields("number", "communityId"), r.communityHandler.CommunityRemove)
		}
	}
	routes = eng.Group("/label")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/chat", r.jidValidationMiddleware.ValidateNumberField(), r.labelHandler.ChatLabel)
			routes.POST("/message", r.labelHandler.MessageLabel)
			routes.POST("/edit", r.labelHandler.EditLabel)
			routes.GET("/list", r.labelHandler.GetLabels)
		}
	}
	routes = eng.Group("/unlabel")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/chat", r.jidValidationMiddleware.ValidateNumberField(), r.labelHandler.ChatUnlabel)
			routes.POST("/message", r.labelHandler.MessageUnlabel)
		}
	}
	routes = eng.Group("/newsletter")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/create", r.newsletterHandler.CreateNewsletter)
			routes.GET("/list", r.newsletterHandler.ListNewsletter)
			routes.POST("/info", r.jidValidationMiddleware.ValidateJIDFields("newsletterId"), r.newsletterHandler.GetNewsletter)
			routes.POST("/link", r.jidValidationMiddleware.ValidateJIDFields("newsletterId"), r.newsletterHandler.GetNewsletterInvite)
			routes.POST("/subscribe", r.jidValidationMiddleware.ValidateJIDFields("newsletterId"), r.newsletterHandler.SubscribeNewsletter)
			routes.POST("/messages", r.jidValidationMiddleware.ValidateJIDFields("newsletterId"), r.newsletterHandler.GetNewsletterMessages)
		}
	}

	// NOVO: Rotas de Enquetes (Polls)
	routes = eng.Group("/polls")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.GET("/:pollMessageId/results", r.pollHandler.GetPollResults)
		}
	}

	// Chatwoot: webhook called by the Chatwoot inbox (public, secret in the URL)
	eng.POST("/chatwoot/webhook/:instanceId/:secret", r.chatwootHandler.Webhook)

	routes = eng.Group("/chatwoot")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.GET("/config", r.chatwootHandler.GetConfig)
			routes.PUT("/config", r.chatwootHandler.SetConfig)
			routes.DELETE("/config", r.chatwootHandler.DeleteConfig)
			routes.POST("/import-contacts", r.chatwootHandler.ImportContacts)
		}
	}

	routes = eng.Group("/typebot")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.GET("/config", r.typebotHandler.GetConfig)
			routes.PUT("/config", r.typebotHandler.SetConfig)
			routes.DELETE("/config", r.typebotHandler.DeleteConfig)
			routes.GET("/sessions", r.typebotHandler.ListSessions)
			routes.DELETE("/sessions", r.typebotHandler.CloseSessions)
		}
	}

	// Prometheus scrape: global API key in the apikey header or as a Bearer token.
	eng.GET("/metrics", r.monitoringHandler.Prometheus)

	routes = eng.Group("/monitoring")
	{
		routes.Use(r.authMiddleware.AuthAdmin)
		{
			routes.GET("/dashboard", r.monitoringHandler.Dashboard)
			routes.GET("/events", r.monitoringHandler.Events)
			routes.GET("/alerts", r.monitoringHandler.GetAlerts)
			routes.PUT("/alerts", r.monitoringHandler.SetAlerts)
			routes.POST("/alerts/test", r.monitoringHandler.TestAlert)
		}
	}
}

func NewRouter(
	authMiddleware auth_middleware.Middleware,
	instanceHandler instance_handler.InstanceHandler,
	userHandler user_handler.UserHandler,
	sendHandler send_handler.SendHandler,
	messageHandler message_handler.MessageHandler,
	chatHandler chat_handler.ChatHandler,
	groupHandler group_handler.GroupHandler,
	callHandler call_handler.CallHandler,
	communityHandler community_handler.CommunityHandler,
	labelHandler label_handler.LabelHandler,
	newsletterHandler newsletter_handler.NewsletterHandler,
	pollHandler *poll_handler.PollHandler,
	serverHandler server_handler.ServerHandler,
	chatwootHandler chatwoot_handler.ChatwootHandler,
	typebotHandler typebot_handler.TypebotHandler,
	monitoringHandler monitoring_handler.MonitoringHandler,
	cloudApiHandler *cloudapi.CloudApiHandler,
) *Routes {
	return &Routes{
		authMiddleware:          authMiddleware,
		jidValidationMiddleware: auth_middleware.NewJIDValidationMiddleware(),
		instanceHandler:         instanceHandler,
		userHandler:             userHandler,
		sendHandler:             sendHandler,
		messageHandler:          messageHandler,
		chatHandler:             chatHandler,
		groupHandler:            groupHandler,
		callHandler:             callHandler,
		communityHandler:        communityHandler,
		labelHandler:            labelHandler,
		newsletterHandler:       newsletterHandler,
		pollHandler:             pollHandler,
		serverHandler:           serverHandler,
		chatwootHandler:         chatwootHandler,
		typebotHandler:          typebotHandler,
		monitoringHandler:       monitoringHandler,
		cloudApiHandler:        cloudApiHandler,
	}
}
