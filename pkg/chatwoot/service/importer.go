package chatwoot_service

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	chatwoot_model "github.com/evolution-foundation/evolution-go/pkg/chatwoot/model"
)

// The history import writes straight into the Chatwoot database (like the
// Evolution API does): the Chatwoot API cannot set message timestamps and
// would fire notifications/automations for every old message.

type historyMessage struct {
	WaID      string
	Content   string
	FromMe    bool
	Timestamp time.Time
	SenderJid string
}

type historyChat struct {
	JID      types.JID
	Name     string
	Messages []historyMessage
}

func (s *chatwootService) importDatabase() (*sql.DB, error) {
	s.importOnce.Do(func() {
		db, err := sql.Open("postgres", s.importDBURI)
		if err != nil {
			s.importErr = err
			return
		}
		db.SetMaxOpenConns(5)
		db.SetConnMaxIdleTime(time.Minute)
		s.importDB = db
	})
	return s.importDB, s.importErr
}

func (s *chatwootService) importHistory(cfg *chatwoot_model.ChatwootConfig, instanceID string, client *whatsmeow.Client, evt *events.HistorySync) {
	log := s.loggerWrapper.GetLogger(instanceID)

	db, err := s.importDatabase()
	if err != nil {
		log.LogError("[%s] [chatwoot] import database unavailable: %v", instanceID, err)
		return
	}

	// DaysLimitImport 0 imports everything WhatsApp sent in the history sync.
	var cutoff time.Time
	if cfg.DaysLimitImport > 0 {
		cutoff = time.Now().AddDate(0, 0, -cfg.DaysLimitImport)
	}
	ignore := parseIgnoreList(cfg.IgnoreJids)

	var chats []historyChat
	skipped := 0 // groups, newsletters, ignored numbers, unparsable ids
	for _, conv := range evt.Data.GetConversations() {
		rawJID, err := types.ParseJID(conv.GetID())
		if err != nil {
			skipped++
			continue
		}
		chatJID := rawJID.ToNonAD()
		if chatJID.Server == types.HiddenUserServer {
			if pn, err := types.ParseJID(conv.GetPnJID()); err == nil && pn.Server == types.DefaultUserServer {
				chatJID = pn.ToNonAD()
			} else if pn, err := client.Store.LIDs.GetPNForLID(context.Background(), chatJID); err == nil && !pn.IsEmpty() {
				chatJID = pn.ToNonAD()
			}
		}
		if !isSupportedChat(chatJID) || isIgnored(ignore, chatJID) {
			skipped++
			continue
		}

		chat := historyChat{JID: chatJID, Name: conv.GetName()}
		if chat.Name == "" {
			chat.Name = conv.GetDisplayName()
		}

		for _, hm := range conv.GetMessages() {
			web := hm.GetMessage()
			if web == nil {
				continue
			}
			parsed, err := client.ParseWebMessage(rawJID, web)
			if err != nil || parsed.Info.Timestamp.Before(cutoff) {
				continue
			}
			msg := parsed.Message
			if msg.GetProtocolMessage() != nil || msg.GetReactionMessage() != nil {
				continue
			}

			content := textContent(msg)
			if media := extractMedia(msg, parsed.Info.ID); media != nil {
				content = strings.TrimSpace(mediaPlaceholder(msg) + "\n" + media.Caption)
			}
			if content == "" {
				continue
			}

			if chat.Name == "" && !parsed.Info.IsFromMe {
				chat.Name = web.GetPushName()
			}
			chat.Messages = append(chat.Messages, historyMessage{
				WaID:      parsed.Info.ID,
				Content:   content,
				FromMe:    parsed.Info.IsFromMe,
				Timestamp: parsed.Info.Timestamp,
				SenderJid: parsed.Info.Sender.ToNonAD().String(),
			})
		}

		if cfg.ImportMessages && len(chat.Messages) == 0 && !cfg.ImportContacts {
			continue
		}
		chats = append(chats, chat)
	}

	syncType := evt.Data.GetSyncType().String()
	if len(chats) == 0 {
		log.LogInfo("[%s] [chatwoot] history import (%s): nothing to import (%d conversations skipped: groups/ignored)", instanceID, syncType, skipped)
		return
	}
	log.LogInfo("[%s] [chatwoot] history import (%s): importing %d chats (%d skipped: groups/ignored)", instanceID, syncType, len(chats), skipped)

	ownerCtx, cancelOwner := context.WithTimeout(context.Background(), 30*time.Second)
	userType, userID, err := tokenOwner(ownerCtx, db, cfg.Token)
	cancelOwner()
	if err != nil {
		log.LogError("[%s] [chatwoot] import: could not resolve the token owner: %v", instanceID, err)
		return
	}

	imported, failed := 0, 0
	for i, chat := range chats {
		// Per-chat timeout: a large FULL sync must not be cut by a batch-wide limit.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		n, err := s.importChat(ctx, db, cfg, instanceID, chat, userType, userID)
		cancel()
		if err != nil {
			failed++
			log.LogError("[%s] [chatwoot] import of chat %s failed: %v", instanceID, chat.JID.User, err)
			continue
		}
		imported += n
		s.convCache.Delete(instanceID + "|" + chat.JID.String())
		if (i+1)%50 == 0 {
			log.LogInfo("[%s] [chatwoot] history import (%s): %d/%d chats, %d messages so far", instanceID, syncType, i+1, len(chats), imported)
		}
	}
	log.LogInfo("[%s] [chatwoot] history import (%s) finished: %d chats, %d messages, %d failed", instanceID, syncType, len(chats), imported, failed)
}

func tokenOwner(ctx context.Context, db *sql.DB, token string) (string, int, error) {
	var ownerType string
	var ownerID int
	err := db.QueryRowContext(ctx, `SELECT owner_type, owner_id FROM access_tokens WHERE token = $1`, token).Scan(&ownerType, &ownerID)
	return ownerType, ownerID, err
}

func (s *chatwootService) importChat(ctx context.Context, db *sql.DB, cfg *chatwoot_model.ChatwootConfig, instanceID string, chat historyChat, userType string, userID int) (int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Chatwoot stores "timestamp without time zone" in UTC.
	now := time.Now().UTC()
	identifier := chat.JID.String()
	phone := phoneFromJID(chat.JID)
	phones := []string{}
	if phone != "" {
		phones = []string{phone}
		if cfg.MergeBrazilContacts {
			phones = brazilVariants(phone)
		}
	}

	// Contact
	var contactID int
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM contacts WHERE account_id = $1 AND (identifier = $2 OR phone_number = ANY($3)) ORDER BY id LIMIT 1`,
		cfg.AccountID, identifier, pq.Array(phones)).Scan(&contactID)
	if err == sql.ErrNoRows {
		name := chat.Name
		if name == "" {
			name = strings.TrimPrefix(phone, "+")
		}
		if name == "" {
			name = chat.JID.User
		}
		var phoneArg interface{}
		if phone != "" {
			phoneArg = phone
		}
		err = tx.QueryRowContext(ctx,
			`INSERT INTO contacts (name, phone_number, identifier, account_id, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $5) RETURNING id`,
			name, phoneArg, identifier, cfg.AccountID, now).Scan(&contactID)
	}
	if err != nil {
		return 0, fmt.Errorf("contact: %w", err)
	}

	if !cfg.ImportMessages || len(chat.Messages) == 0 {
		return 0, tx.Commit()
	}

	sort.Slice(chat.Messages, func(i, j int) bool { return chat.Messages[i].Timestamp.Before(chat.Messages[j].Timestamp) })
	first := chat.Messages[0].Timestamp.UTC()
	last := chat.Messages[len(chat.Messages)-1].Timestamp.UTC()

	// Contact inbox
	var contactInboxID int
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM contact_inboxes WHERE contact_id = $1 AND inbox_id = $2 LIMIT 1`,
		contactID, cfg.InboxID).Scan(&contactInboxID)
	if err == sql.ErrNoRows {
		err = tx.QueryRowContext(ctx,
			`INSERT INTO contact_inboxes (contact_id, inbox_id, source_id, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $4) RETURNING id`,
			contactID, cfg.InboxID, uuid.NewString(), now).Scan(&contactInboxID)
	}
	if err != nil {
		return 0, fmt.Errorf("contact inbox: %w", err)
	}

	// Conversation (display_id is assigned by Chatwoot's insert trigger)
	var conversationID, displayID int
	err = tx.QueryRowContext(ctx,
		`SELECT id, display_id FROM conversations WHERE account_id = $1 AND inbox_id = $2 AND contact_id = $3 ORDER BY id DESC LIMIT 1`,
		cfg.AccountID, cfg.InboxID, contactID).Scan(&conversationID, &displayID)
	if err == sql.ErrNoRows {
		err = tx.QueryRowContext(ctx,
			`INSERT INTO conversations (account_id, inbox_id, status, contact_id, contact_inbox_id, uuid, last_activity_at, created_at, updated_at)
			 VALUES ($1, $2, 0, $3, $4, $5, $6, $7, $6) RETURNING id, display_id`,
			cfg.AccountID, cfg.InboxID, contactID, contactInboxID, uuid.NewString(), last, first).Scan(&conversationID, &displayID)
	}
	if err != nil {
		return 0, fmt.Errorf("conversation: %w", err)
	}

	// Skip messages already present (re-sync, or mirrored live)
	sourceIDs := make([]string, len(chat.Messages))
	for i, m := range chat.Messages {
		sourceIDs[i] = sourceIDPrefix + m.WaID
	}
	existing := map[string]bool{}
	rows, err := tx.QueryContext(ctx, `SELECT source_id FROM messages WHERE conversation_id = $1 AND source_id = ANY($2)`, conversationID, pq.Array(sourceIDs))
	if err != nil {
		return 0, fmt.Errorf("existing messages: %w", err)
	}
	for rows.Next() {
		var sid string
		if rows.Scan(&sid) == nil {
			existing[sid] = true
		}
	}
	rows.Close()

	var mappings []chatwoot_model.ChatwootMessage
	for _, m := range chat.Messages {
		sourceID := sourceIDPrefix + m.WaID
		if existing[sourceID] {
			continue
		}
		messageType, senderType, senderID := 0, "Contact", contactID
		if m.FromMe {
			messageType, senderType, senderID = 1, userType, userID
		}
		var messageID int
		err = tx.QueryRowContext(ctx,
			`INSERT INTO messages (content, processed_message_content, account_id, inbox_id, conversation_id, message_type, private, sender_type, sender_id, source_id, created_at, updated_at)
			 VALUES ($1, $1, $2, $3, $4, $5, false, $6, $7, $8, $9, $9) RETURNING id`,
			m.Content, cfg.AccountID, cfg.InboxID, conversationID, messageType, senderType, senderID, sourceID, m.Timestamp.UTC()).Scan(&messageID)
		if err != nil {
			return 0, fmt.Errorf("message: %w", err)
		}
		mappings = append(mappings, chatwoot_model.ChatwootMessage{
			InstanceID:        instanceID,
			WaMessageID:       m.WaID,
			ChatwootMessageID: messageID,
			ConversationID:    displayID,
			ChatJid:           chat.JID.String(),
			SenderJid:         m.SenderJid,
			FromMe:            m.FromMe,
		})
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE conversations SET last_activity_at = GREATEST(last_activity_at, $2) WHERE id = $1`,
		conversationID, last); err != nil {
		return 0, fmt.Errorf("conversation activity: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	for i := range mappings {
		_ = s.repo.SaveMessage(&mappings[i])
	}
	return len(mappings), nil
}
