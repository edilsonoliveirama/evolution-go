package monitoring_service

import (
	"sort"
	"time"

	monitoring_model "github.com/evolution-foundation/evolution-go/pkg/monitoring/model"
)

const MaxDashboardDays = 365

type Dashboard struct {
	Days          int       `json:"days"`
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
	Timezone      string    `json:"timezone"`
	RetentionDays int       `json:"retentionDays"`

	Totals    Totals                      `json:"totals"`
	Daily     []DayPoint                  `json:"daily"`
	Hourly    []HourPoint                 `json:"hourly,omitempty"` // only for periods of up to 2 days
	Types     []TypeCount                 `json:"types"`
	Failures  []ComponentCount            `json:"failures"`
	Heatmap   []monitoring_model.HeatCell `json:"heatmap"`
	Instances []InstanceHealth            `json:"instances"`
}

type Totals struct {
	MessagesIn    int64 `json:"messagesIn"`
	MessagesOut   int64 `json:"messagesOut"`
	NewContacts   int64 `json:"newContacts"`
	ActiveChats   int64 `json:"activeChats"`
	Delivered     int64 `json:"delivered"`
	Read          int64 `json:"read"`
	Failures      int64 `json:"failures"`
	Disconnects   int64 `json:"disconnects"`
	LoggedOuts    int64 `json:"loggedOuts"`
	Bans          int64 `json:"bans"`
	Connected     int   `json:"connected"`
	InstanceCount int   `json:"instanceCount"`
}

type DayPoint struct {
	Day         string `json:"day"`
	In          int64  `json:"in"`
	Out         int64  `json:"out"`
	NewContacts int64  `json:"newContacts"`
	ActiveChats int64  `json:"activeChats"`
	Failures    int64  `json:"failures"`
}

type HourPoint struct {
	Hour string `json:"hour"`
	In   int64  `json:"in"`
	Out  int64  `json:"out"`
}

type TypeCount struct {
	Type string `json:"type"`
	In   int64  `json:"in"`
	Out  int64  `json:"out"`
}

type ComponentCount struct {
	Component string `json:"component"`
	Count     int64  `json:"count"`
}

type InstanceHealth struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Connected     bool       `json:"connected"`
	DownSince     *time.Time `json:"downSince,omitempty"`
	UptimePercent *float64   `json:"uptimePercent"` // nil: no connection data in the period
	Disconnects   int64      `json:"disconnects"`
	LoggedOuts    int64      `json:"loggedOuts"`
	Bans          int64      `json:"bans"`
	Failures      int64      `json:"failures"`
	MessagesIn    int64      `json:"messagesIn"`
	MessagesOut   int64      `json:"messagesOut"`
}

// EventView is a connection or failure event for the timeline.
type EventView struct {
	monitoring_model.MetricEvent
	InstanceName string `json:"instanceName"`
}

func (s *monitoringService) Dashboard(days int, instanceID string) (*Dashboard, error) {
	if days < 1 {
		days = 7
	}
	if days > MaxDashboardDays {
		days = MaxDashboardDays
	}
	now := s.now()
	local := now.In(s.loc)
	from := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, s.loc).AddDate(0, 0, -(days - 1))
	fromDay, toDay := from.Format("2006-01-02"), local.Format("2006-01-02")

	d := &Dashboard{Days: days, From: from, To: now, Timezone: s.tz, RetentionDays: int(s.retention.Hours() / 24)}

	instances, err := s.instanceRepository.GetAll(s.clientName)
	if err != nil {
		return nil, err
	}
	health := map[string]*InstanceHealth{}
	var ids []string
	downSince := s.alerts.downSince()
	for _, inst := range instances {
		if instanceID != "" && inst.Id != instanceID {
			continue
		}
		h := &InstanceHealth{ID: inst.Id, Name: inst.Name, Connected: s.isOnline(inst.Id)}
		if t, ok := downSince[inst.Id]; ok && !h.Connected {
			t := t
			h.DownSince = &t
		}
		if h.Connected {
			d.Totals.Connected++
		}
		health[inst.Id] = h
		ids = append(ids, inst.Id)
	}
	d.Totals.InstanceCount = len(ids)

	// Totals per day (kept forever).
	daily, err := s.repo.Daily(fromDay, toDay, instanceID)
	if err != nil {
		return nil, err
	}
	points := map[string]*DayPoint{}
	for day := from; !day.After(local); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		points[key] = &DayPoint{Day: key}
		d.Daily = append(d.Daily, DayPoint{Day: key})
	}
	types := map[string]*TypeCount{}
	failures := map[string]int64{}
	for _, row := range daily {
		p := points[row.Day]
		h := health[row.InstanceID]
		switch row.Kind {
		case monitoring_model.KindMessageIn, monitoring_model.KindMessageOut:
			t := types[row.Detail]
			if t == nil {
				t = &TypeCount{Type: row.Detail}
				types[row.Detail] = t
			}
			if row.Kind == monitoring_model.KindMessageIn {
				d.Totals.MessagesIn += row.Count
				t.In += row.Count
				if p != nil {
					p.In += row.Count
				}
				if h != nil {
					h.MessagesIn += row.Count
				}
			} else {
				d.Totals.MessagesOut += row.Count
				t.Out += row.Count
				if p != nil {
					p.Out += row.Count
				}
				if h != nil {
					h.MessagesOut += row.Count
				}
			}
		case monitoring_model.KindDelivered:
			d.Totals.Delivered += row.Count
		case monitoring_model.KindRead:
			d.Totals.Read += row.Count
		case monitoring_model.KindFailure:
			d.Totals.Failures += row.Count
			failures[row.Detail] += row.Count
			if p != nil {
				p.Failures += row.Count
			}
			if h != nil {
				h.Failures += row.Count
			}
		case monitoring_model.KindDisconnected, monitoring_model.KindConnectFailure, monitoring_model.KindStreamReplaced:
			d.Totals.Disconnects += row.Count
			if h != nil {
				h.Disconnects += row.Count
			}
		case monitoring_model.KindLoggedOut:
			d.Totals.LoggedOuts += row.Count
			if h != nil {
				h.LoggedOuts += row.Count
			}
		case monitoring_model.KindTemporaryBan:
			d.Totals.Bans += row.Count
			if h != nil {
				h.Bans += row.Count
			}
		}
	}

	// Contacts and chats.
	newContacts, err := s.repo.NewContactsPerDay(from, now, instanceID, s.tz)
	if err != nil {
		return nil, err
	}
	for _, c := range newContacts {
		d.Totals.NewContacts += c.Count
		if p := points[c.Day]; p != nil {
			p.NewContacts = c.Count
		}
	}
	activePerDay, err := s.repo.ActiveChatsPerDay(from, now, instanceID, s.tz)
	if err != nil {
		return nil, err
	}
	for _, c := range activePerDay {
		if p := points[c.Day]; p != nil {
			p.ActiveChats = c.Count
		}
	}
	if d.Totals.ActiveChats, err = s.repo.ActiveChats(from, now, instanceID); err != nil {
		return nil, err
	}
	for i := range d.Daily {
		d.Daily[i] = *points[d.Daily[i].Day]
	}

	if days <= 2 {
		hourly, err := s.repo.HourlyMessages(from, now, instanceID, s.tz)
		if err != nil {
			return nil, err
		}
		d.Hourly = hourlyPoints(from, local, hourly)
	}

	if d.Heatmap, err = s.repo.Heatmap(from, now, instanceID, s.tz); err != nil {
		return nil, err
	}

	for _, t := range types {
		d.Types = append(d.Types, *t)
	}
	sort.Slice(d.Types, func(i, j int) bool { return d.Types[i].In+d.Types[i].Out > d.Types[j].In+d.Types[j].Out })
	for c, n := range failures {
		d.Failures = append(d.Failures, ComponentCount{Component: c, Count: n})
	}
	sort.Slice(d.Failures, func(i, j int) bool { return d.Failures[i].Count > d.Failures[j].Count })

	// Uptime from the connection events.
	before, err := s.repo.LastStatesBefore(from)
	if err != nil {
		return nil, err
	}
	during, err := s.repo.StateEvents(from, now)
	if err != nil {
		return nil, err
	}
	uptime := uptimeByInstance(ids, before, during, from, now)
	for _, id := range ids {
		h := health[id]
		h.UptimePercent = uptime[id]
		d.Instances = append(d.Instances, *h)
	}
	sort.Slice(d.Instances, func(i, j int) bool { return d.Instances[i].Name < d.Instances[j].Name })

	return d, nil
}

func hourlyPoints(from, to time.Time, rows []monitoring_model.BucketCount) []HourPoint {
	byHour := map[string]int{}
	var out []HourPoint
	for h := from; !h.After(to); h = h.Add(time.Hour) {
		key := h.Format("2006-01-02T15:00")
		byHour[key] = len(out)
		out = append(out, HourPoint{Hour: key})
	}
	for _, r := range rows {
		i, ok := byHour[r.Bucket]
		if !ok {
			continue
		}
		if r.Kind == monitoring_model.KindMessageIn {
			out[i].In += r.Count
		} else {
			out[i].Out += r.Count
		}
	}
	return out
}

// uptimeByInstance is the percentage of [from, to) each instance was
// connected, counting only the time its state is known.
func uptimeByInstance(ids []string, before, during []monitoring_model.MetricEvent, from, to time.Time) map[string]*float64 {
	type state struct {
		known, up   bool
		since       time.Time
		upDur, knwn time.Duration
	}
	states := make(map[string]*state, len(ids))
	for _, id := range ids {
		states[id] = &state{since: from}
	}

	var lastStart time.Time
	for _, e := range before {
		if e.Kind == monitoring_model.KindServiceStart {
			lastStart = e.CreatedAt
		}
	}
	if !lastStart.IsZero() {
		// Offline from the last restart until a Connected event says otherwise.
		for _, st := range states {
			st.known = true
		}
	}
	for _, e := range before {
		st := states[e.InstanceID]
		if st == nil || e.CreatedAt.Before(lastStart) {
			continue
		}
		st.known = true
		st.up = e.Kind == monitoring_model.KindConnected
	}

	advance := func(st *state, t time.Time) {
		if st.known && t.After(st.since) {
			d := t.Sub(st.since)
			st.knwn += d
			if st.up {
				st.upDur += d
			}
		}
		if t.After(st.since) {
			st.since = t
		}
	}
	for _, e := range during {
		up := e.Kind == monitoring_model.KindConnected
		if e.InstanceID == "" {
			for _, st := range states {
				advance(st, e.CreatedAt)
				st.known, st.up = true, false
			}
			continue
		}
		if st := states[e.InstanceID]; st != nil {
			advance(st, e.CreatedAt)
			st.known, st.up = true, up
		}
	}

	out := make(map[string]*float64, len(ids))
	for id, st := range states {
		advance(st, to)
		if st.knwn > 0 {
			pct := float64(st.upDur) / float64(st.knwn) * 100
			out[id] = &pct
		}
	}
	return out
}

func (s *monitoringService) Events(instanceID string, limit int) ([]EventView, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	kinds := []string{
		monitoring_model.KindConnected, monitoring_model.KindDisconnected, monitoring_model.KindLoggedOut,
		monitoring_model.KindConnectFailure, monitoring_model.KindTemporaryBan, monitoring_model.KindStreamReplaced,
		monitoring_model.KindPaired, monitoring_model.KindServiceStart, monitoring_model.KindFailure,
	}
	rows, err := s.repo.RecentEvents(instanceID, kinds, limit)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	if instances, err := s.instanceRepository.GetAll(s.clientName); err == nil {
		for _, inst := range instances {
			names[inst.Id] = inst.Name
		}
	}
	out := make([]EventView, len(rows))
	for i, r := range rows {
		out[i] = EventView{MetricEvent: r, InstanceName: names[r.InstanceID]}
	}
	return out, nil
}
