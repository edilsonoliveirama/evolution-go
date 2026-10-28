// Package metrics holds process-lifetime, in-memory counters for the
// dashboard stats endpoint. Independent from pkg/core's license heartbeat
// counters, which reset every heartbeat interval and are not meant for
// operator-facing reporting.
package metrics

import (
	"sync/atomic"
	"time"
)

var (
	messagesSent     atomic.Int64
	messagesReceived atomic.Int64
	startTime        = time.Now()
)

func RecordMessageSent() {
	messagesSent.Add(1)
}

func RecordMessageReceived() {
	messagesReceived.Add(1)
}

func MessagesSent() int64 {
	return messagesSent.Load()
}

func MessagesReceived() int64 {
	return messagesReceived.Load()
}

func Uptime() time.Duration {
	return time.Since(startTime)
}

// Components reported to RecordDeliveryFailure.
const (
	FailureWebhook  = "webhook"
	FailureRabbitMQ = "rabbitmq"
	FailureNats     = "nats"
	FailureChatwoot = "chatwoot"
	FailureTypebot  = "typebot"
	FailureSend     = "send"
)

var failureHook atomic.Value // func(instanceID, component string)

// OnDeliveryFailure registers the receiver of RecordDeliveryFailure calls
// (the monitoring service, which stores them and raises alerts).
func OnDeliveryFailure(fn func(instanceID, component string)) {
	failureHook.Store(fn)
}

// RecordDeliveryFailure reports a message or event that could not be
// delivered (webhook, queue, integration or WhatsApp send).
func RecordDeliveryFailure(instanceID, component string) {
	if fn, ok := failureHook.Load().(func(string, string)); ok && fn != nil {
		fn(instanceID, component)
	}
}
