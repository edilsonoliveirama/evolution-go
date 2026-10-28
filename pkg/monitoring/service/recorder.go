package monitoring_service

import (
	"time"

	"github.com/gomessguii/logger"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	instance_model "github.com/evolution-foundation/evolution-go/pkg/instance/model"
	monitoring_model "github.com/evolution-foundation/evolution-go/pkg/monitoring/model"
)

const (
	bufferSize    = 10000
	flushEvery    = 5 * time.Second
	flushMaxBatch = 1000
	pruneEvery    = 6 * time.Hour
	maxDetailLen  = 64
)

// HandleWhatsappEvent implements whatsmeow_service.EventListener. It only
// classifies the event: storage happens on the flush goroutine.
func (s *monitoringService) HandleWhatsappEvent(instance *instance_model.Instance, _ *whatsmeow.Client, evt interface{}) {
	if instance == nil {
		return
	}
	now := s.now()
	ev := monitoring_model.MetricEvent{InstanceID: instance.Id, CreatedAt: now}

	switch e := evt.(type) {
	case *events.Message:
		if ignoredChat(e.Info.Chat) {
			return
		}
		msgType := MessageType(e.Message)
		if msgType == "" {
			return
		}
		ev.Kind = monitoring_model.KindMessageIn
		if e.Info.IsFromMe {
			ev.Kind = monitoring_model.KindMessageOut
		}
		ev.Detail = msgType
		ev.Chat = e.Info.Chat.User
		ev.IsGroup = e.Info.IsGroup
		// Messages received while offline arrive later: count them when they were sent.
		if ts := e.Info.Timestamp; !ts.IsZero() && ts.Before(now) && now.Sub(ts) < s.retention {
			ev.CreatedAt = ts
		}
	case *events.Receipt:
		// Receipts from our other devices or from group participants say nothing about delivery.
		if e.IsFromMe || e.IsGroup || ignoredChat(e.Chat) {
			return
		}
		switch e.Type {
		case types.ReceiptTypeDelivered:
			ev.Kind = monitoring_model.KindDelivered
		case types.ReceiptTypeRead:
			ev.Kind = monitoring_model.KindRead
		default:
			return
		}
		ev.Chat = e.Chat.User
		for range e.MessageIDs {
			s.record(ev)
		}
		return
	case *events.Connected:
		ev.Kind = monitoring_model.KindConnected
	case *events.Disconnected:
		ev.Kind = monitoring_model.KindDisconnected
	case *events.LoggedOut:
		ev.Kind = monitoring_model.KindLoggedOut
		ev.Detail = e.Reason.String()
	case *events.ConnectFailure:
		ev.Kind = monitoring_model.KindConnectFailure
		ev.Detail = e.Reason.String()
	case *events.TemporaryBan:
		ev.Kind = monitoring_model.KindTemporaryBan
		ev.Detail = e.Code.String()
	case *events.StreamReplaced:
		ev.Kind = monitoring_model.KindStreamReplaced
	case *events.PairSuccess:
		ev.Kind = monitoring_model.KindPaired
	default:
		return
	}
	s.record(ev)
}

// RecordFailure receives metrics.RecordDeliveryFailure calls.
func (s *monitoringService) RecordFailure(instanceID, component string) {
	if instanceID == "" {
		return
	}
	s.record(monitoring_model.MetricEvent{
		InstanceID: instanceID,
		Kind:       monitoring_model.KindFailure,
		Detail:     component,
		CreatedAt:  s.now(),
	})
}

// record never blocks: it is called from the whatsmeow event goroutines.
func (s *monitoringService) record(ev monitoring_model.MetricEvent) {
	if len(ev.Detail) > maxDetailLen {
		ev.Detail = ev.Detail[:maxDetailLen]
	}
	s.counters.add(ev)
	if alerts := s.alerts.observe(ev, s.alertSettings()); len(alerts) > 0 {
		go s.deliver(alerts)
	}
	select {
	case s.events <- ev:
	default:
		if s.dropped.Add(1)%1000 == 1 {
			logger.LogWarn("[monitoring] event buffer full, dropping events (%d so far)", s.dropped.Load())
		}
	}
}

func (s *monitoringService) flushLoop() {
	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()
	batch := make([]monitoring_model.MetricEvent, 0, flushMaxBatch)
	for {
		select {
		case ev := <-s.events:
			batch = append(batch, ev)
			if len(batch) >= flushMaxBatch {
				s.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				s.flush(batch)
				batch = batch[:0]
			}
		}
	}
}

func (s *monitoringService) flush(batch []monitoring_model.MetricEvent) {
	daily, contacts := aggregate(batch, s.loc)
	if err := s.repo.Write(batch, daily, contacts); err != nil {
		logger.LogError("[monitoring] failed to store %d events: %v", len(batch), err)
	}
}

func (s *monitoringService) pruneLoop() {
	for {
		if n, err := s.repo.PruneEvents(s.now().Add(-s.retention)); err != nil {
			logger.LogError("[monitoring] failed to prune old events: %v", err)
		} else if n > 0 {
			logger.LogInfo("[monitoring] pruned %d events older than %s", n, s.retention)
		}
		time.Sleep(pruneEvery)
	}
}

// aggregate builds the per-day totals and the first contact of each
// individual chat in a batch.
func aggregate(batch []monitoring_model.MetricEvent, loc *time.Location) ([]monitoring_model.MetricDaily, []monitoring_model.MetricContact) {
	type dayKey struct{ day, instance, kind, detail string }
	type chatKey struct{ instance, chat string }
	days := map[dayKey]int64{}
	firstSeen := map[chatKey]time.Time{}

	for _, ev := range batch {
		detail := ""
		switch ev.Kind {
		case monitoring_model.KindMessageIn, monitoring_model.KindMessageOut, monitoring_model.KindFailure:
			// Low cardinality: message type or failure component. Disconnect reasons stay in the raw events.
			detail = ev.Detail
		}
		days[dayKey{ev.CreatedAt.In(loc).Format("2006-01-02"), ev.InstanceID, ev.Kind, detail}]++

		isMessage := ev.Kind == monitoring_model.KindMessageIn || ev.Kind == monitoring_model.KindMessageOut
		if isMessage && !ev.IsGroup && ev.Chat != "" {
			k := chatKey{ev.InstanceID, ev.Chat}
			if t, ok := firstSeen[k]; !ok || ev.CreatedAt.Before(t) {
				firstSeen[k] = ev.CreatedAt
			}
		}
	}

	daily := make([]monitoring_model.MetricDaily, 0, len(days))
	for k, n := range days {
		daily = append(daily, monitoring_model.MetricDaily{Day: k.day, InstanceID: k.instance, Kind: k.kind, Detail: k.detail, Count: n})
	}
	contacts := make([]monitoring_model.MetricContact, 0, len(firstSeen))
	for k, t := range firstSeen {
		contacts = append(contacts, monitoring_model.MetricContact{InstanceID: k.instance, Chat: k.chat, FirstSeen: t})
	}
	return daily, contacts
}

func ignoredChat(chat types.JID) bool {
	return chat.Server == types.BroadcastServer || chat.Server == types.NewsletterServer || chat.IsEmpty()
}

// MessageType classifies a message for the metrics. It returns "" for
// messages that are not conversation content (edits, revokes, poll votes,
// key distribution...).
func MessageType(m *waE2E.Message) string {
	if m == nil {
		return ""
	}
	switch {
	case m.GetProtocolMessage() != nil, m.GetPollUpdateMessage() != nil,
		m.GetEncReactionMessage() != nil, m.GetKeepInChatMessage() != nil:
		return ""
	case m.Conversation != nil, m.GetExtendedTextMessage() != nil:
		return "text"
	case m.GetImageMessage() != nil:
		return "image"
	case m.GetVideoMessage() != nil, m.GetPtvMessage() != nil:
		return "video"
	case m.GetAudioMessage() != nil:
		return "audio"
	case m.GetDocumentMessage() != nil, m.GetDocumentWithCaptionMessage() != nil:
		return "document"
	case m.GetStickerMessage() != nil:
		return "sticker"
	case m.GetLocationMessage() != nil, m.GetLiveLocationMessage() != nil:
		return "location"
	case m.GetContactMessage() != nil, m.GetContactsArrayMessage() != nil:
		return "contact"
	case m.GetReactionMessage() != nil:
		return "reaction"
	case m.GetPollCreationMessage() != nil, m.GetPollCreationMessageV2() != nil, m.GetPollCreationMessageV3() != nil:
		return "poll"
	case m.GetButtonsMessage() != nil, m.GetListMessage() != nil, m.GetTemplateMessage() != nil,
		m.GetInteractiveMessage() != nil, m.GetButtonsResponseMessage() != nil,
		m.GetListResponseMessage() != nil, m.GetTemplateButtonReplyMessage() != nil,
		m.GetInteractiveResponseMessage() != nil:
		return "interactive"
	case m.GetSenderKeyDistributionMessage() != nil:
		// Key distribution with no content of its own.
		return ""
	}
	return "other"
}
