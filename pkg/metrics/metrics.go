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
