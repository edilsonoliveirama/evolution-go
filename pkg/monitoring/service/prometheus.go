package monitoring_service

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/evolution-foundation/evolution-go/pkg/metrics"
	monitoring_model "github.com/evolution-foundation/evolution-go/pkg/monitoring/model"
)

// counters are the Prometheus counters, since the process start.
type counters struct {
	mu       sync.Mutex
	messages map[[3]string]int64 // instance, direction, type
	events   map[[2]string]int64 // instance, kind
	failures map[[2]string]int64 // instance, component
	receipts map[[2]string]int64 // instance, status
}

func newCounters() *counters {
	return &counters{
		messages: map[[3]string]int64{},
		events:   map[[2]string]int64{},
		failures: map[[2]string]int64{},
		receipts: map[[2]string]int64{},
	}
}

func (c *counters) add(ev monitoring_model.MetricEvent) {
	if ev.InstanceID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch ev.Kind {
	case monitoring_model.KindMessageIn:
		c.messages[[3]string{ev.InstanceID, "in", ev.Detail}]++
	case monitoring_model.KindMessageOut:
		c.messages[[3]string{ev.InstanceID, "out", ev.Detail}]++
	case monitoring_model.KindFailure:
		c.failures[[2]string{ev.InstanceID, ev.Detail}]++
	case monitoring_model.KindDelivered, monitoring_model.KindRead:
		c.receipts[[2]string{ev.InstanceID, ev.Kind}]++
	default:
		c.events[[2]string{ev.InstanceID, ev.Kind}]++
	}
}

// WritePrometheus writes the metrics in the Prometheus text format.
func (s *monitoringService) WritePrometheus(w io.Writer) error {
	instances, err := s.instanceRepository.GetAll(s.clientName)
	if err != nil {
		return err
	}
	names := map[string]string{}
	ids := make([]string, 0, len(instances))
	for _, inst := range instances {
		names[inst.Id] = inst.Name
		ids = append(ids, inst.Id)
	}
	sort.Strings(ids)

	b := bufio.NewWriter(w)
	inst := func(id string) string {
		return fmt.Sprintf(`instance_id="%s",instance_name="%s"`, escapeLabel(id), escapeLabel(names[id]))
	}

	header(b, "evogo_instance_connected", "gauge", "1 when the instance is connected and logged in to WhatsApp.")
	for _, id := range ids {
		v := 0
		if s.isOnline(id) {
			v = 1
		}
		fmt.Fprintf(b, "evogo_instance_connected{%s} %d\n", inst(id), v)
	}

	c := s.counters
	c.mu.Lock()
	header(b, "evogo_messages_total", "counter", "Messages received (in) and sent (out), by message type.")
	for _, k := range sortedKeys3(c.messages) {
		fmt.Fprintf(b, "evogo_messages_total{%s,direction=\"%s\",type=\"%s\"} %d\n", inst(k[0]), k[1], escapeLabel(k[2]), c.messages[k])
	}
	header(b, "evogo_connection_events_total", "counter", "Connection events (connected, disconnected, logged_out, ...).")
	for _, k := range sortedKeys2(c.events) {
		fmt.Fprintf(b, "evogo_connection_events_total{%s,event=\"%s\"} %d\n", inst(k[0]), k[1], c.events[k])
	}
	header(b, "evogo_delivery_failures_total", "counter", "Messages or events that could not be delivered, by component.")
	for _, k := range sortedKeys2(c.failures) {
		fmt.Fprintf(b, "evogo_delivery_failures_total{%s,component=\"%s\"} %d\n", inst(k[0]), escapeLabel(k[1]), c.failures[k])
	}
	header(b, "evogo_receipts_total", "counter", "Delivery and read receipts of sent messages (individual chats).")
	for _, k := range sortedKeys2(c.receipts) {
		fmt.Fprintf(b, "evogo_receipts_total{%s,status=\"%s\"} %d\n", inst(k[0]), k[1], c.receipts[k])
	}
	c.mu.Unlock()

	header(b, "evogo_monitoring_dropped_events_total", "counter", "Monitoring events dropped because the write buffer was full.")
	fmt.Fprintf(b, "evogo_monitoring_dropped_events_total %d\n", s.dropped.Load())
	header(b, "evogo_process_uptime_seconds", "gauge", "Seconds since the Evolution GO process started.")
	fmt.Fprintf(b, "evogo_process_uptime_seconds %d\n", int64(metrics.Uptime().Seconds()))

	return b.Flush()
}

func header(w io.Writer, name, typ, help string) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func escapeLabel(s string) string { return labelEscaper.Replace(s) }

func sortedKeys2(m map[[2]string]int64) [][2]string {
	keys := make([][2]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i][0]+"\x00"+keys[i][1] < keys[j][0]+"\x00"+keys[j][1] })
	return keys
}

func sortedKeys3(m map[[3]string]int64) [][3]string {
	keys := make([][3]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return strings.Join(keys[i][:], "\x00") < strings.Join(keys[j][:], "\x00")
	})
	return keys
}
