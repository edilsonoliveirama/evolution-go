package chatwoot_service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/patrickmn/go-cache"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
	instance_model "github.com/evolution-foundation/evolution-go/pkg/instance/model"
)

// HandleWhatsappEvent receives every whatsmeow event of every instance (and
// the messages sent through the API) and mirrors the relevant ones to Chatwoot.
func (s *chatwootService) HandleWhatsappEvent(instance *instance_model.Instance, client *whatsmeow.Client, rawEvt interface{}) {
	if instance == nil || client == nil {
		return
	}
	cfg := s.activeConfig(instance.Id)
	if cfg == nil {
		return
	}

	switch evt := rawEvt.(type) {
	case *events.Message:
		chat := s.resolveChat(client, evt)
		if !isSupportedChat(chat) || isIgnored(parseIgnoreList(cfg.IgnoreJids), chat) {
			return
		}
		s.enqueue(instance.Id, chat.String(), func() {
			s.handleMessage(cfg, instance.Id, client, chat, evt)
		})
	case *events.Receipt:
		if evt.Type != types.ReceiptTypeRead || evt.IsGroup || !isSupportedChat(evt.Chat) || len(evt.MessageIDs) == 0 {
			return
		}
		s.enqueue(instance.Id, evt.Chat.String(), func() {
			s.handleReadReceipt(cfg, instance.Id, client, evt)
		})
	case *events.HistorySync:
		log := s.loggerWrapper.GetLogger(instance.Id)
		syncType := evt.Data.GetSyncType().String()
		switch {
		case len(evt.Data.GetConversations()) == 0:
			// push names, status, etc.: nothing to import
		case !s.ImportAvailable():
			log.LogInfo("[%s] [chatwoot] history import skipped (%s): CHATWOOT_IMPORT_DATABASE_URI not set", instance.Id, syncType)
		case !cfg.ImportMessages && !cfg.ImportContacts:
			log.LogInfo("[%s] [chatwoot] history import skipped (%s): import is disabled in the Chatwoot settings", instance.Id, syncType)
		default:
			log.LogInfo("[%s] [chatwoot] history import queued (%s): %d conversations", instance.Id, syncType, len(evt.Data.GetConversations()))
			s.enqueue(instance.Id, "history", func() {
				s.importHistory(cfg, instance.Id, client, evt)
			})
		}
	case *events.AppStateSyncComplete:
		// Contacts arrive with the critical_unblock_low patch right after pairing.
		if cfg.ImportContacts && evt.Name == appstate.WAPatchCriticalUnblockLow {
			key := "contacts-imported|" + instance.Id
			if _, done := s.throttle.Get(key); !done {
				s.throttle.Set(key, true, 24*time.Hour)
				go func() {
					if err := s.ImportContacts(instance.Id); err != nil {
						s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] [chatwoot] contact import failed: %v", instance.Id, err)
					}
				}()
			}
		}
	}
}

// resolveChat returns the phone-number JID of the chat when WhatsApp addressed
// it by LID, so the Chatwoot contact is keyed by the phone number.
func (s *chatwootService) resolveChat(client *whatsmeow.Client, evt *events.Message) types.JID {
	chat := evt.Info.Chat.ToNonAD()
	if chat.Server != types.HiddenUserServer {
		return chat
	}

	alt := evt.Info.SenderAlt
	if evt.Info.IsFromMe {
		alt = evt.Info.RecipientAlt
	}
	if alt.Server == types.DefaultUserServer {
		return alt.ToNonAD()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if pn, err := client.Store.LIDs.GetPNForLID(ctx, chat); err == nil && !pn.IsEmpty() {
		return pn.ToNonAD()
	}
	return chat
}

func (s *chatwootService) handleMessage(cfg *chatwoot_model.ChatwootConfig, instanceID string, client *whatsmeow.Client, chat types.JID, evt *events.Message) {
	log := s.loggerWrapper.GetLogger(instanceID)
	waID := evt.Info.ID

	if _, sentByChatwoot := s.pending.Get(instanceID + "|" + waID); sentByChatwoot {
		return
	}
	if existing, err := s.repo.GetMessageByWaID(instanceID, waID); err == nil && existing != nil {
		return
	}

	msg := evt.Message
	if pm := msg.GetProtocolMessage(); pm != nil {
		switch pm.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			s.handleRevoke(cfg, instanceID, pm.GetKey().GetID())
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			s.handleEdit(cfg, instanceID, client, chat, evt, pm)
		}
		return
	}

	content := textContent(msg)
	replyToWaID := ""
	if ci := contextInfo(msg); ci != nil {
		replyToWaID = ci.GetStanzaID()
	}

	if r := msg.GetReactionMessage(); r != nil {
		if r.GetText() == "" {
			return // reaction removed
		}
		content = r.GetText()
		replyToWaID = r.GetKey().GetID()
	}

	var attachment *cwAttachment
	if media := extractMedia(msg, waID); media != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		data, err := client.Download(ctx, media.Downloadable)
		cancel()
		if err != nil {
			log.LogError("[%s] [chatwoot] failed to download media %s: %v", instanceID, waID, err)
			content = strings.TrimSpace(mediaPlaceholder(msg) + " _(não foi possível baixar a mídia)_\n" + media.Caption)
		} else {
			attachment = &cwAttachment{FileName: media.FileName, MimeType: media.MimeType, Data: data}
			content = media.Caption
		}
	}

	if content == "" && attachment == nil {
		return // poll votes, pins, keep-in-chat and other non-displayable events
	}

	out := cwCreateMessage{
		Content:     content,
		MessageType: "incoming",
		SourceID:    sourceIDPrefix + waID,
		Attachment:  attachment,
	}
	if evt.Info.IsFromMe {
		out.MessageType = "outgoing"
	}
	if replyToWaID != "" {
		if quoted, err := s.repo.GetMessageByWaID(instanceID, replyToWaID); err == nil && quoted != nil {
			out.ContentAttributes = map[string]interface{}{
				"in_reply_to":             quoted.ChatwootMessageID,
				"in_reply_to_external_id": sourceIDPrefix + replyToWaID,
			}
		}
	}

	pushName := ""
	if !evt.Info.IsFromMe {
		pushName = evt.Info.PushName
	}

	created, conv, err := s.postMessage(cfg, instanceID, client, chat, pushName, out)
	if err != nil {
		log.LogError("[%s] [chatwoot] failed to mirror message %s: %v", instanceID, waID, err)
		return
	}

	sender := evt.Info.Sender.ToNonAD()
	if evt.Info.IsFromMe && client.Store.ID != nil {
		sender = client.Store.ID.ToNonAD()
	}
	if err := s.repo.SaveMessage(&chatwoot_model.ChatwootMessage{
		InstanceID:        instanceID,
		WaMessageID:       waID,
		ChatwootMessageID: created.ID,
		ConversationID:    conv.ConversationID,
		ChatJid:           chat.String(),
		SenderJid:         sender.String(),
		FromMe:            evt.Info.IsFromMe,
	}); err != nil {
		log.LogError("[%s] [chatwoot] failed to save message mapping: %v", instanceID, err)
	}
	log.LogInfo("[%s] [chatwoot] %s message %s mirrored to conversation %d", instanceID, out.MessageType, waID, conv.ConversationID)
}

// postMessage creates the message in the chat's conversation, recreating the
// conversation once if the cached one no longer exists in Chatwoot.
func (s *chatwootService) postMessage(cfg *chatwoot_model.ChatwootConfig, instanceID string, client *whatsmeow.Client, chat types.JID, pushName string, out cwCreateMessage) (*cwMessage, *convRef, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	for attempt := 0; attempt < 2; attempt++ {
		conv, err := s.ensureConversation(ctx, cfg, instanceID, client, chat, pushName)
		if err != nil {
			return nil, nil, err
		}
		created, err := s.api(cfg).CreateMessage(ctx, conv.ConversationID, out)
		if errors.Is(err, ErrNotFound) && attempt == 0 {
			s.convCache.Delete(instanceID + "|" + chat.String())
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		return created, conv, nil
	}
	return nil, nil, errors.New("conversation not found")
}

// ensureConversation finds or creates the Chatwoot contact and conversation
// for a WhatsApp chat.
func (s *chatwootService) ensureConversation(ctx context.Context, cfg *chatwoot_model.ChatwootConfig, instanceID string, client *whatsmeow.Client, chat types.JID, pushName string) (*convRef, error) {
	key := instanceID + "|" + chat.String()
	lock := s.convLock(key)
	lock.Lock()
	defer lock.Unlock()

	if v, ok := s.convCache.Get(key); ok {
		return v.(*convRef), nil
	}

	api := s.api(cfg)
	contact, sourceID, err := s.ensureContact(ctx, cfg, client, chat, pushName)
	if err != nil {
		return nil, fmt.Errorf("contact: %w", err)
	}

	conversations, err := api.ContactConversations(ctx, contact.ID)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}

	var picked *cwConversation
	for i := range conversations {
		c := &conversations[i]
		if c.InboxID != cfg.InboxID {
			continue
		}
		if !cfg.ReopenConversation && c.Status == "resolved" {
			continue
		}
		if picked == nil || c.ID > picked.ID {
			picked = c
		}
	}

	if picked == nil {
		status := ""
		if cfg.ConversationPending {
			status = "pending"
		}
		created, createErr := api.CreateConversation(ctx, contact.ID, cfg.InboxID, sourceID, status)
		if createErr != nil {
			// Chatwoot may fail after inserting (e.g. a broken callback answers 500);
			// reuse what was created instead of duplicating it on every retry.
			if existing, err := api.ContactConversations(ctx, contact.ID); err == nil {
				for i := range existing {
					if existing[i].InboxID == cfg.InboxID && (created == nil || existing[i].ID > created.ID) {
						created = &existing[i]
					}
				}
			}
			if created == nil {
				return nil, fmt.Errorf("create conversation: %w", createErr)
			}
			s.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] [chatwoot] create conversation failed (%v), reusing conversation %d", instanceID, createErr, created.ID)
		}
		picked = created
	} else if picked.Status == "resolved" {
		// Chatwoot only reopens on incoming messages; reopen explicitly so
		// messages sent from the phone also land in an open conversation.
		status := "open"
		if cfg.ConversationPending {
			status = "pending"
		}
		if err := api.ToggleConversationStatus(ctx, picked.ID, status); err != nil {
			s.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] [chatwoot] failed to reopen conversation %d: %v", instanceID, picked.ID, err)
		}
	}

	ref := &convRef{ContactID: contact.ID, ConversationID: picked.ID, SourceID: sourceID}
	s.convCache.Set(key, ref, cache.DefaultExpiration)
	return ref, nil
}

func (s *chatwootService) ensureContact(ctx context.Context, cfg *chatwoot_model.ChatwootConfig, client *whatsmeow.Client, chat types.JID, pushName string) (*cwContact, string, error) {
	api := s.api(cfg)
	identifier := chat.String()
	phone := phoneFromJID(chat)

	contact, err := s.findContact(ctx, cfg, chat)
	if err != nil {
		return nil, "", err
	}

	if contact == nil {
		name := pushName
		if name == "" {
			name = strings.TrimPrefix(phone, "+")
		}
		if name == "" {
			name = chat.User
		}
		avatar := ""
		if client != nil {
			picCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if pic, err := client.GetProfilePictureInfo(picCtx, chat, &whatsmeow.GetProfilePictureParams{}); err == nil && pic != nil {
				avatar = pic.URL
			}
			cancel()
		}

		created, sourceID, err := api.CreateContact(ctx, cfg.InboxID, name, phone, identifier, avatar)
		if err != nil {
			return nil, "", err
		}
		if sourceID == "" {
			if sourceID, err = api.CreateContactInbox(ctx, created.ID, cfg.InboxID); err != nil {
				return nil, "", err
			}
		}
		return created, sourceID, nil
	}

	// Replace the placeholder name (the number) once we learn the push name.
	if pushName != "" && (contact.Name == strings.TrimPrefix(contact.PhoneNumber, "+") || contact.Name == contact.PhoneNumber || contact.Name == chat.User) {
		if err := api.UpdateContact(ctx, contact.ID, map[string]interface{}{"name": pushName}); err == nil {
			contact.Name = pushName
		}
	}

	for _, ci := range contact.ContactInboxes {
		if ci.Inbox.ID == cfg.InboxID && ci.SourceID != "" {
			return contact, ci.SourceID, nil
		}
	}
	sourceID, err := api.CreateContactInbox(ctx, contact.ID, cfg.InboxID)
	if err != nil {
		return nil, "", err
	}
	return contact, sourceID, nil
}

func (s *chatwootService) findContact(ctx context.Context, cfg *chatwoot_model.ChatwootConfig, chat types.JID) (*cwContact, error) {
	api := s.api(cfg)
	identifier := chat.String()
	phone := phoneFromJID(chat)

	phones := []string{}
	if phone != "" {
		phones = []string{phone}
		if cfg.MergeBrazilContacts {
			phones = brazilVariants(phone)
		}
	}

	queries := append([]string{}, phones...)
	if len(queries) == 0 {
		queries = []string{chat.User}
	}

	for _, q := range queries {
		results, err := api.SearchContacts(ctx, strings.TrimPrefix(q, "+"))
		if err != nil {
			return nil, err
		}
		for i := range results {
			c := &results[i]
			if c.Identifier == identifier {
				return c, nil
			}
			for _, p := range phones {
				if c.PhoneNumber == p {
					return c, nil
				}
			}
		}
	}
	return nil, nil
}

func (s *chatwootService) handleRevoke(cfg *chatwoot_model.ChatwootConfig, instanceID, waID string) {
	mapping, err := s.repo.GetMessageByWaID(instanceID, waID)
	if err != nil || mapping == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.api(cfg).DeleteMessage(ctx, mapping.ConversationID, mapping.ChatwootMessageID); err != nil && !errors.Is(err, ErrNotFound) {
		s.loggerWrapper.GetLogger(instanceID).LogError("[%s] [chatwoot] failed to delete message %d: %v", instanceID, mapping.ChatwootMessageID, err)
	}
}

func (s *chatwootService) handleEdit(cfg *chatwoot_model.ChatwootConfig, instanceID string, client *whatsmeow.Client, chat types.JID, evt *events.Message, pm *waE2E.ProtocolMessage) {
	newText := textContent(pm.GetEditedMessage())
	if newText == "" {
		if img := pm.GetEditedMessage().GetImageMessage(); img != nil {
			newText = img.GetCaption()
		} else if vid := pm.GetEditedMessage().GetVideoMessage(); vid != nil {
			newText = vid.GetCaption()
		}
	}
	if newText == "" {
		return
	}

	out := cwCreateMessage{
		Content:     "✏️ _Mensagem editada:_\n" + newText,
		MessageType: "incoming",
		SourceID:    sourceIDPrefix + evt.Info.ID,
	}
	if evt.Info.IsFromMe {
		out.MessageType = "outgoing"
	}
	if orig, err := s.repo.GetMessageByWaID(instanceID, pm.GetKey().GetID()); err == nil && orig != nil {
		out.ContentAttributes = map[string]interface{}{"in_reply_to": orig.ChatwootMessageID}
	}

	created, conv, err := s.postMessage(cfg, instanceID, client, chat, "", out)
	if err != nil {
		s.loggerWrapper.GetLogger(instanceID).LogError("[%s] [chatwoot] failed to mirror edit: %v", instanceID, err)
		return
	}
	_ = s.repo.SaveMessage(&chatwoot_model.ChatwootMessage{
		InstanceID:        instanceID,
		WaMessageID:       evt.Info.ID,
		ChatwootMessageID: created.ID,
		ConversationID:    conv.ConversationID,
		ChatJid:           chat.String(),
		FromMe:            evt.Info.IsFromMe,
	})
}

// handleReadReceipt marks the agent messages as seen by the contact.
func (s *chatwootService) handleReadReceipt(cfg *chatwoot_model.ChatwootConfig, instanceID string, client *whatsmeow.Client, evt *events.Receipt) {
	if cfg.InboxIdentifier == "" {
		return
	}
	var mapping *chatwoot_model.ChatwootMessage
	for i := len(evt.MessageIDs) - 1; i >= 0 && mapping == nil; i-- {
		mapping, _ = s.repo.GetMessageByWaID(instanceID, evt.MessageIDs[i])
	}
	if mapping == nil {
		return
	}

	throttleKey := fmt.Sprintf("seen|%s|%d", instanceID, mapping.ConversationID)
	if _, recent := s.throttle.Get(throttleKey); recent {
		return
	}
	s.throttle.Set(throttleKey, true, 10*time.Second)

	chat, err := types.ParseJID(mapping.ChatJid)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conv, err := s.ensureConversation(ctx, cfg, instanceID, client, chat, "")
	if err != nil || conv.SourceID == "" {
		return
	}
	if err := s.api(cfg).UpdateLastSeen(ctx, cfg.InboxIdentifier, conv.SourceID, mapping.ConversationID); err != nil {
		s.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] [chatwoot] failed to update last seen: %v", instanceID, err)
	}
}

// ImportContacts creates a Chatwoot contact for every named WhatsApp contact.
func (s *chatwootService) ImportContacts(instanceID string) error {
	cfg := s.activeConfig(instanceID)
	if cfg == nil {
		return errors.New("integração com o Chatwoot não está ativa")
	}
	client := s.clientPointer[instanceID]
	if client == nil || !client.IsLoggedIn() {
		return errors.New("instância não está conectada ao WhatsApp")
	}

	ctx := context.Background()
	contacts, err := client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return err
	}

	log := s.loggerWrapper.GetLogger(instanceID)
	created := 0
	for jid, info := range contacts {
		if jid.Server != types.DefaultUserServer {
			continue
		}
		name := info.FullName
		if name == "" {
			name = info.BusinessName
		}
		if name == "" {
			name = info.PushName
		}
		if name == "" {
			continue
		}

		// Long enough to wait out a full Chatwoot rate-limit window (Retry-After ~60s).
		reqCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		existing, err := s.findContact(reqCtx, cfg, jid)
		if err == nil && existing == nil {
			if _, _, err = s.api(cfg).CreateContact(reqCtx, cfg.InboxID, name, phoneFromJID(jid), jid.String(), ""); err == nil {
				created++
			}
		}
		cancel()
		if err != nil {
			log.LogWarn("[%s] [chatwoot] failed to import contact %s: %v", instanceID, jid.User, err)
		}
		// ~2 requests per contact; stay well below Chatwoot's rate limit (429s are retried by the client).
		time.Sleep(300 * time.Millisecond)
	}
	log.LogInfo("[%s] [chatwoot] contact import finished: %d created", instanceID, created)
	return nil
}
