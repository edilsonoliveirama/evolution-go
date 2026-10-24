package chatwoot_service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
	send_service "github.com/evolution-foundation/evolution-go/pkg/sendMessage/service"
)

// ErrUnauthorized is returned when the webhook secret does not match.
var ErrUnauthorized = errors.New("chatwoot: invalid webhook secret")

// flexMessageType accepts both the string ("outgoing") and the integer (1)
// encodings Chatwoot uses for message_type in different payloads.
type flexMessageType string

func (f *flexMessageType) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err == nil {
		*f = flexMessageType(str)
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	switch n {
	case 0:
		*f = "incoming"
	case 1:
		*f = "outgoing"
	case 2:
		*f = "activity"
	case 3:
		*f = "template"
	}
	return nil
}

type cwWebhookMeta struct {
	Sender struct {
		Identifier  string `json:"identifier"`
		PhoneNumber string `json:"phone_number"`
	} `json:"sender"`
}

type cwWebhook struct {
	Event             string                 `json:"event"`
	ID                int                    `json:"id"`
	Content           string                 `json:"content"`
	Status            string                 `json:"status"` // conversation_status_changed
	Meta              cwWebhookMeta          `json:"meta"`   // conversation_status_changed
	MessageType       flexMessageType        `json:"message_type"`
	Private           bool                   `json:"private"`
	SourceID          string                 `json:"source_id"`
	ContentAttributes map[string]interface{} `json:"content_attributes"`
	Sender            struct {
		Name          string `json:"name"`
		AvailableName string `json:"available_name"`
		Type          string `json:"type"`
	} `json:"sender"`
	Conversation struct {
		ID   int           `json:"id"`
		Meta cwWebhookMeta `json:"meta"`
	} `json:"conversation"`
	Attachments []struct {
		FileType string `json:"file_type"`
		DataURL  string `json:"data_url"`
	} `json:"attachments"`
}

func (s *chatwootService) HandleWebhook(instanceID, secret string, body []byte) error {
	cfg, err := s.repo.GetConfig(instanceID)
	if err != nil {
		return err
	}
	if cfg == nil || cfg.WebhookSecret == "" || subtle.ConstantTimeCompare([]byte(cfg.WebhookSecret), []byte(secret)) != 1 {
		return ErrUnauthorized
	}
	if !cfg.Enabled {
		return nil
	}

	var hook cwWebhook
	if err := json.Unmarshal(body, &hook); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}

	switch hook.Event {
	case "message_created":
		if hook.MessageType != "outgoing" || hook.Private || strings.HasPrefix(hook.SourceID, sourceIDPrefix) {
			return nil
		}
	case "message_updated":
		if deleted, _ := hook.ContentAttributes["deleted"].(bool); !deleted {
			return nil
		}
	case "conversation_status_changed":
		if hook.Status == "resolved" && s.onResolved != nil {
			if chat, err := metaJID(hook.Meta); err == nil {
				s.onResolved(instanceID, chat)
			}
		}
		return nil
	default:
		return nil
	}

	key := strconv.Itoa(hook.Conversation.ID)
	s.enqueue(instanceID, "cw|"+key, func() {
		if hook.Event == "message_created" {
			s.sendToWhatsapp(cfg, instanceID, &hook)
		} else {
			s.revokeOnWhatsapp(instanceID, hook.ID)
		}
	})
	return nil
}

func (s *chatwootService) sendToWhatsapp(cfg *chatwoot_model.ChatwootConfig, instanceID string, hook *cwWebhook) {
	log := s.loggerWrapper.GetLogger(instanceID)

	// Messages we created ourselves come back through the webhook; skip them.
	if existing, err := s.repo.GetMessageByChatwootID(instanceID, hook.ID); err == nil && existing != nil {
		return
	}

	failed := func(err error) {
		log.LogError("[%s] [chatwoot] failed to send Chatwoot message %d to WhatsApp: %v", instanceID, hook.ID, err)
		s.privateNote(cfg, hook.Conversation.ID, "⚠️ Não foi possível enviar a mensagem para o WhatsApp: "+err.Error())
	}

	chat, err := destinationJID(hook)
	if err != nil {
		failed(err)
		return
	}

	client := s.clientPointer[instanceID]
	if client == nil || !client.IsLoggedIn() {
		failed(errors.New("instância desconectada do WhatsApp"))
		return
	}
	instance, err := s.instanceRepo.GetInstanceByID(instanceID)
	if err != nil {
		failed(err)
		return
	}

	text := markdownToWhatsApp(hook.Content)
	if cfg.SignMessages {
		name := hook.Sender.AvailableName
		if name == "" {
			name = hook.Sender.Name
		}
		if strings.TrimSpace(text) != "" || len(hook.Attachments) == 0 {
			text = signMessage(text, name, cfg.SignDelimiter)
		}
	}

	quoted := s.quotedFor(instanceID, hook)
	number := chat.User
	if chat.Server != types.DefaultUserServer {
		number = chat.String()
	}

	record := func(waID string) {
		_ = s.repo.SaveMessage(&chatwoot_model.ChatwootMessage{
			InstanceID:        instanceID,
			WaMessageID:       waID,
			ChatwootMessageID: hook.ID,
			ConversationID:    hook.Conversation.ID,
			ChatJid:           chat.String(),
			SenderJid:         client.Store.ID.ToNonAD().String(),
			FromMe:            true,
		})
	}

	if len(hook.Attachments) == 0 {
		if strings.TrimSpace(text) == "" {
			return
		}
		waID := s.reserveID(instanceID, client)
		if _, err := s.sendService.SendText(&send_service.TextStruct{Number: number, Text: text, Id: waID, Quoted: quoted}, instance); err != nil {
			failed(err)
			return
		}
		record(waID)
		return
	}

	caption := text
	for i, att := range hook.Attachments {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		data, contentType, err := s.api(cfg).Download(ctx, att.DataURL)
		cancel()
		if err != nil {
			failed(err)
			continue
		}

		mediaType := whatsappMediaType(att.FileType, baseMime(contentType), data)
		media := &send_service.MediaStruct{
			Number:   number,
			Type:     mediaType,
			Filename: attachmentName(att.DataURL, i),
			Id:       s.reserveID(instanceID, client),
		}
		if i == 0 {
			media.Quoted = quoted
			// WhatsApp audio has no caption; send the text separately below.
			if mediaType != "audio" {
				media.Caption = caption
				caption = ""
			}
		}

		if _, err := s.sendService.SendMediaFile(media, data, instance); err != nil {
			failed(err)
			continue
		}
		record(media.Id)
	}

	if strings.TrimSpace(caption) != "" {
		waID := s.reserveID(instanceID, client)
		if _, err := s.sendService.SendText(&send_service.TextStruct{Number: number, Text: caption, Id: waID}, instance); err != nil {
			failed(err)
			return
		}
		record(waID)
	}
}

// reserveID pre-generates the WhatsApp message ID and marks it as sent by
// Chatwoot, so the echo of our own send is not mirrored back.
func (s *chatwootService) reserveID(instanceID string, client *whatsmeow.Client) string {
	id := client.GenerateMessageID()
	s.pending.SetDefault(instanceID+"|"+id, true)
	return id
}

func (s *chatwootService) quotedFor(instanceID string, hook *cwWebhook) send_service.QuotedStruct {
	var mapping *chatwoot_model.ChatwootMessage

	if ext, ok := hook.ContentAttributes["in_reply_to_external_id"].(string); ok && strings.HasPrefix(ext, sourceIDPrefix) {
		mapping, _ = s.repo.GetMessageByWaID(instanceID, strings.TrimPrefix(ext, sourceIDPrefix))
	}
	if mapping == nil {
		if replyTo, ok := hook.ContentAttributes["in_reply_to"].(float64); ok && replyTo > 0 {
			mapping, _ = s.repo.GetMessageByChatwootID(instanceID, int(replyTo))
		}
	}
	if mapping == nil {
		return send_service.QuotedStruct{}
	}
	return send_service.QuotedStruct{MessageID: mapping.WaMessageID, Participant: mapping.SenderJid}
}

func (s *chatwootService) revokeOnWhatsapp(instanceID string, chatwootMessageID int) {
	mappings, err := s.repo.GetMessagesByChatwootID(instanceID, chatwootMessageID)
	if err != nil || len(mappings) == 0 {
		return
	}
	client := s.clientPointer[instanceID]
	if client == nil || !client.IsLoggedIn() {
		return
	}
	for _, m := range mappings {
		if !m.FromMe {
			continue // only our own messages can be deleted for everyone
		}
		chat, err := types.ParseJID(m.ChatJid)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err = client.SendMessage(ctx, chat, client.BuildRevoke(chat, types.EmptyJID, m.WaMessageID))
		cancel()
		if err != nil {
			s.loggerWrapper.GetLogger(instanceID).LogError("[%s] [chatwoot] failed to revoke %s: %v", instanceID, m.WaMessageID, err)
		}
	}
}

func (s *chatwootService) privateNote(cfg *chatwoot_model.ChatwootConfig, conversationID int, text string) {
	if conversationID == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _ = s.api(cfg).CreateMessage(ctx, conversationID, cwCreateMessage{Content: text, MessageType: "outgoing", Private: true})
}

func destinationJID(hook *cwWebhook) (types.JID, error) {
	return metaJID(hook.Conversation.Meta)
}

func metaJID(meta cwWebhookMeta) (types.JID, error) {
	sender := meta.Sender
	if strings.Contains(sender.Identifier, "@") {
		if jid, err := types.ParseJID(sender.Identifier); err == nil && isSupportedChat(jid) {
			return jid.ToNonAD(), nil
		}
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, sender.PhoneNumber)
	if digits == "" {
		return types.EmptyJID, errors.New("contato sem número de WhatsApp")
	}
	return types.NewJID(digits, types.DefaultUserServer), nil
}

// whatsappMediaType maps a Chatwoot attachment to the send_service media type,
// falling back to document for formats WhatsApp would reject as media.
func whatsappMediaType(fileType, mime string, data []byte) string {
	if mime == "" || mime == "application/octet-stream" {
		mime = http.DetectContentType(data)
	}
	switch fileType {
	case "image":
		if mime == "image/jpeg" || mime == "image/png" || mime == "image/webp" {
			return "image"
		}
	case "video":
		if mime == "video/mp4" {
			return "video"
		}
	case "audio":
		return "audio"
	}
	return "document"
}

func attachmentName(dataURL string, index int) string {
	if u, err := url.Parse(dataURL); err == nil {
		if name, err := url.PathUnescape(path.Base(u.Path)); err == nil && name != "" && name != "/" && name != "." {
			return name
		}
	}
	return fmt.Sprintf("arquivo-%d", index+1)
}
