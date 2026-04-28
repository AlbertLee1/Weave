package subscriptions

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/google/uuid"

	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/oss/where"
)

// AggregateSubscribeRequest is the payload of a { type: "subscribeAggregation" }
// message. It scopes a single named metric (count / sum / avg / min / max) over
// one ObjectType, optionally filtered by Where and split into buckets by
// GroupBy (exact-match field name only). On every matching change event the
// hub recomputes the affected bucket using delta math against the previous
// snapshot of the same primaryKey — a full Bleve scan is performed only once
// at subscribe time.
type AggregateSubscribeRequest struct {
	ObjectType string             `json:"objectType"`
	Where      *where.WhereClause `json:"where,omitempty"`
	Metric     AggMetric          `json:"metric"`
	GroupBy    string             `json:"groupBy,omitempty"`
}

// AggMetric names a single output value computed by an aggregation
// subscription. Type ∈ {count, sum, avg, min, max}; Field is required for
// every type other than count. Name defaults to Type when blank.
type AggMetric struct {
	Type  string `json:"type"`
	Field string `json:"field,omitempty"`
	Name  string `json:"name,omitempty"`
}

// validateAggMetric checks the metric shape; field is required for non-count.
func validateAggMetric(m AggMetric) error {
	switch m.Type {
	case "count":
		return nil
	case "sum", "avg", "min", "max":
		if m.Field == "" {
			return fmt.Errorf("metric %q requires field", m.Type)
		}
		return nil
	default:
		return fmt.Errorf("unsupported metric type: %q (supported: count, sum, avg, min, max)", m.Type)
	}
}

// metricName returns the user-supplied name, falling back to the metric type
// so emitted rows always carry a stable, human-readable key.
func (m AggMetric) metricName() string {
	if m.Name != "" {
		return m.Name
	}
	return m.Type
}

// IndexResolver looks up the Bleve index backing an objectType. *pkg/index.Manager
// satisfies this contract via its GetIndex method. nil from GetIndex means the
// index does not exist yet — the aggregator treats that as "no initial state"
// and starts empty.
type IndexResolver interface {
	GetIndex(objectType string) bleve.Index
}

// aggSnapshot is the per-primaryKey memo the aggregator keeps so it can revert
// the contribution of an object when the same object is later updated or
// deleted. Without the snapshot, sum/min/max/avg would have no way to know
// what value to subtract from the running totals.
type aggSnapshot struct {
	matched  bool        // the snapshot satisfied Where at the time it was recorded
	groupKey interface{} // the bucket the snapshot belongs to (nil = null bucket / no groupBy)
	hasValue bool        // metric value was present and numeric
	value    float64     // metric value (ignored when metric type is "count")
}

// groupAggState is the running state for a single bucket. count/sum carry
// integer/floating-point totals; minMax tracks a multiset of seen values so
// reverting an addition does not silently corrupt the cached extremum.
type groupAggState struct {
	count  int64
	sum    float64
	minMax *minMaxMultiset
}

// minMaxMultiset tracks how many times each value is currently in the bucket
// so that reverting an addition (decrementing the count of a particular
// value) keeps the running min / max honest. When the count of the current
// minimum or maximum hits zero the cached extremum is invalidated and
// recomputed from the remaining keys on next read.
type minMaxMultiset struct {
	counts map[float64]int64
}

func newMinMaxMultiset() *minMaxMultiset {
	return &minMaxMultiset{counts: make(map[float64]int64)}
}

func (m *minMaxMultiset) add(v float64) {
	m.counts[v]++
}

// remove decrements the running count for v; the entry is deleted entirely
// when its count reaches zero so min/max remain accurate. Removing a value
// that was never added is a no-op, mirroring the snapshot-revert semantics.
func (m *minMaxMultiset) remove(v float64) {
	c, ok := m.counts[v]
	if !ok {
		return
	}
	c--
	if c <= 0 {
		delete(m.counts, v)
		return
	}
	m.counts[v] = c
}

func (m *minMaxMultiset) empty() bool {
	return len(m.counts) == 0
}

func (m *minMaxMultiset) min() float64 {
	first := true
	var out float64
	for v := range m.counts {
		if first || v < out {
			out = v
			first = false
		}
	}
	return out
}

func (m *minMaxMultiset) max() float64 {
	first := true
	var out float64
	for v := range m.counts {
		if first || v > out {
			out = v
			first = false
		}
	}
	return out
}

// IncrementalAggregator keeps the running state of one aggregation
// subscription. Every call to Apply mutates the state in place using the
// delta from the snapshot rather than re-running the full aggregation.
type IncrementalAggregator struct {
	mu         sync.Mutex
	objectType string
	whereCl    *where.WhereClause
	metric     AggMetric
	groupBy    string
	snapshots  map[string]*aggSnapshot
	groups     map[interface{}]*groupAggState
}

// newIncrementalAggregator allocates an empty aggregator. Initial state is
// populated by Seed (via the Bleve index walk) or grown lazily via Apply.
func newIncrementalAggregator(objectType string, w *where.WhereClause, metric AggMetric, groupBy string) *IncrementalAggregator {
	return &IncrementalAggregator{
		objectType: objectType,
		whereCl:    w,
		metric:     metric,
		groupBy:    groupBy,
		snapshots:  make(map[string]*aggSnapshot),
		groups:     make(map[interface{}]*groupAggState),
	}
}

// Apply updates the running totals for a single ChangeEvent. ADDED_OR_UPDATED
// reverts any previous contribution from the same primaryKey then re-applies
// the new contribution if Where is still satisfied. DELETED reverts and
// removes the snapshot. Returns true when the call mutated state and the
// caller should emit an aggregationChanged event.
//
// state is the WebSocket subscription event state — "ADDED_OR_UPDATED" or
// "DELETED" — to keep the aggregator decoupled from funnel.EditType.
func (a *IncrementalAggregator) Apply(state, objectType, primaryKey string, properties map[string]interface{}) bool {
	if objectType != a.objectType {
		return false
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	prev := a.snapshots[primaryKey]
	if prev != nil && prev.matched {
		a.removeContribution(prev)
	}

	if state == "DELETED" {
		delete(a.snapshots, primaryKey)
		// A delete only matters if we previously knew about this PK.
		return prev != nil && prev.matched
	}

	matched := where.MatchClause(a.whereCl, properties)
	groupKey := a.extractGroupKey(properties)
	value, hasValue := a.extractMetricValue(properties)

	if matched {
		a.addContribution(groupKey, value, hasValue)
	}

	a.snapshots[primaryKey] = &aggSnapshot{
		matched:  matched,
		groupKey: groupKey,
		hasValue: hasValue,
		value:    value,
	}

	// Mutated state when we either reverted a prior contribution or added a
	// new one. Pure non-matching updates of objects that never matched are a
	// no-op and don't deserve a wire event.
	prevMatched := prev != nil && prev.matched
	return prevMatched || matched
}

// addContribution applies a single (group, value) addition to the running
// state. Caller holds a.mu. For sum/avg/min/max a missing-or-non-numeric value
// still increments count but is otherwise ignored — matching the convention
// used by the standard aggregation engine.
func (a *IncrementalAggregator) addContribution(groupKey interface{}, value float64, hasValue bool) {
	g, ok := a.groups[groupKey]
	if !ok {
		g = &groupAggState{}
		if a.metric.Type == "min" || a.metric.Type == "max" {
			g.minMax = newMinMaxMultiset()
		}
		a.groups[groupKey] = g
	}
	g.count++
	if hasValue {
		g.sum += value
		if g.minMax != nil {
			g.minMax.add(value)
		}
	}
}

// removeContribution reverses a previous (group, value) addition. When a
// bucket's count drops to zero the bucket is deleted entirely so the response
// shape stays stable across full add → full delete cycles. Caller holds a.mu.
func (a *IncrementalAggregator) removeContribution(snap *aggSnapshot) {
	g, ok := a.groups[snap.groupKey]
	if !ok {
		return
	}
	if g.count > 0 {
		g.count--
	}
	if snap.hasValue {
		g.sum -= snap.value
		if g.minMax != nil {
			g.minMax.remove(snap.value)
		}
	}
	if g.count == 0 {
		delete(a.groups, snap.groupKey)
	}
}

// extractGroupKey returns the bucket key for an object's properties. nil is
// the canonical "null bucket" key when GroupBy is empty (no grouping) or the
// requested field is absent. Numeric / string / bool are kept as-is so the
// final group payload mirrors the source value's JSON shape.
func (a *IncrementalAggregator) extractGroupKey(properties map[string]interface{}) interface{} {
	if a.groupBy == "" {
		return nil
	}
	v, ok := properties[a.groupBy]
	if !ok {
		return nil
	}
	switch v.(type) {
	case nil:
		return nil
	case string, bool, float64, float32, int, int64, int32:
		return v
	default:
		// Maps / slices / opaque types collapse to their JSON-stringified form
		// so the bucket key remains hashable. Should be rare in practice but
		// avoids a runtime panic from `m[unhashable]`.
		buf, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		return string(buf)
	}
}

// extractMetricValue returns the numeric value of the metric field from the
// properties map. count metrics ignore the field entirely. Booleans coerce
// to 0/1 (matches the JSON shape of `bleve` numeric facets); strings parse as
// floats when possible. Anything else returns hasValue=false so the running
// sum / extremum is left alone.
func (a *IncrementalAggregator) extractMetricValue(properties map[string]interface{}) (float64, bool) {
	if a.metric.Type == "count" {
		return 0, false
	}
	v, ok := properties[a.metric.Field]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

// Seed walks the supplied Bleve index to seed the aggregator with the current
// state of the underlying ObjectType. It is invoked once at subscribe time.
// The index may be nil (no rows known yet) — we treat that as "start empty"
// and let Apply grow the state as change events arrive.
func (a *IncrementalAggregator) Seed(idx bleve.Index, scanLimit int) error {
	if idx == nil {
		return nil
	}
	if scanLimit <= 0 {
		scanLimit = 10000
	}
	q := bleve.NewMatchAllQuery()
	req := bleve.NewSearchRequest(q)
	req.Size = scanLimit
	req.Fields = []string{"*"}
	res, err := idx.Search(req)
	if err != nil {
		return fmt.Errorf("aggregator seed search: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	for _, hit := range res.Hits {
		props := hit.Fields
		if props == nil {
			props = map[string]interface{}{}
		}
		matched := where.MatchClause(a.whereCl, props)
		groupKey := a.extractGroupKey(props)
		value, hasValue := a.extractMetricValue(props)
		if matched {
			a.addContribution(groupKey, value, hasValue)
		}
		a.snapshots[hit.ID] = &aggSnapshot{
			matched:  matched,
			groupKey: groupKey,
			hasValue: hasValue,
			value:    value,
		}
	}
	return nil
}

// Snapshot returns the current aggregation result in the same shape used by
// the on-demand /aggregate endpoint. Rows are ordered by stringified group
// key so consecutive snapshots are stable for diffing on the client.
func (a *IncrementalAggregator) Snapshot() *aggregation.AggregationResponse {
	a.mu.Lock()
	defer a.mu.Unlock()

	entries := make([]groupResponseEntry, 0, len(a.groups))
	for k, v := range a.groups {
		entries = append(entries, groupResponseEntry{key: k, state: v})
	}
	// Sort: nil last; everything else by stringified value ascending. Mirrors
	// the engine's sortGroupEntries convention.
	sortKVForResponse(entries)

	rows := make([]aggregation.AggregationRow, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, aggregation.AggregationRow{
			Group:   a.groupForResponse(e.key),
			Metrics: []aggregation.MetricValue{a.metricFor(e.state)},
		})
	}
	return &aggregation.AggregationResponse{
		Data:     rows,
		Accuracy: "ACCURATE",
	}
}

// groupForResponse builds the row's `group` map. When GroupBy is empty we
// omit the field entirely (the row represents the grand total); when the
// resolved key is nil we still emit `{[groupBy]: nil}` so the client can
// distinguish "object had no value for field" from "no rows at all".
func (a *IncrementalAggregator) groupForResponse(key interface{}) map[string]interface{} {
	if a.groupBy == "" {
		return nil
	}
	return map[string]interface{}{a.groupBy: key}
}

// metricFor projects a single bucket's running totals into the wire shape.
// avg returns 0 when count == 0 (matches the engine path); min/max return 0
// when the multiset is empty. count never coerces to a float — it's an int64
// on the wire so client code can rely on numeric-integer comparisons.
func (a *IncrementalAggregator) metricFor(g *groupAggState) aggregation.MetricValue {
	name := a.metric.metricName()
	switch a.metric.Type {
	case "count":
		return aggregation.MetricValue{Name: name, Value: g.count}
	case "sum":
		return aggregation.MetricValue{Name: name, Value: g.sum}
	case "avg":
		if g.count == 0 {
			return aggregation.MetricValue{Name: name, Value: float64(0)}
		}
		return aggregation.MetricValue{Name: name, Value: g.sum / float64(g.count)}
	case "min":
		if g.minMax == nil || g.minMax.empty() {
			return aggregation.MetricValue{Name: name, Value: float64(0)}
		}
		return aggregation.MetricValue{Name: name, Value: g.minMax.min()}
	case "max":
		if g.minMax == nil || g.minMax.empty() {
			return aggregation.MetricValue{Name: name, Value: float64(0)}
		}
		return aggregation.MetricValue{Name: name, Value: g.minMax.max()}
	default:
		return aggregation.MetricValue{Name: name, Value: nil}
	}
}

// groupResponseEntry pairs a bucket key with its running state for response
// sorting. The named type lets sortKVForResponse take a concrete slice and
// keeps the call sites readable.
type groupResponseEntry struct {
	key   interface{}
	state *groupAggState
}

// sortKVForResponse mirrors aggregation.sortGroupEntries: non-null values
// ascend by stringified form; nil sinks to the end. Kept in-package because
// the aggregation package's helper is unexported.
func sortKVForResponse(entries []groupResponseEntry) {
	// Two-phase sort: bubble nil entries to the end, alphabetise the rest.
	// Uses a simple insertion since the bucket count is bounded by the
	// cardinality of the GroupBy field which for live aggregations is small.
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0; j-- {
			left := entries[j-1]
			right := entries[j]
			if shouldSwapForResponse(left.key, right.key) {
				entries[j-1], entries[j] = right, left
				continue
			}
			break
		}
	}
}

// shouldSwapForResponse returns true when (a, b) is out of order under the
// response sort: nil at the end, then ascending stringified value.
func shouldSwapForResponse(a, b interface{}) bool {
	aNil := a == nil
	bNil := b == nil
	if aNil && bNil {
		return false
	}
	if aNil {
		// nil should come AFTER b — swap so b moves left.
		return true
	}
	if bNil {
		return false
	}
	return fmt.Sprint(a) > fmt.Sprint(b)
}

// AggregationChangedPayload is the event body for { type: "aggregationChanged" }.
// State carries the full current snapshot so clients render the latest result
// without needing to merge deltas — incremental wins are server-side; the wire
// payload stays stable and easy to consume.
type AggregationChangedPayload struct {
	State *aggregation.AggregationResponse `json:"state"`
}

// newAggregationSubscription wires a Subscription whose Aggregator drives
// matching events. The Definition / ObjectType / Where slots stay nil so the
// hub-level dispatch in HandleObjectChange can branch cleanly on Aggregator.
func newAggregationSubscription(req AggregateSubscribeRequest, agg *IncrementalAggregator) *Subscription {
	return &Subscription{
		ID:         uuid.New().String(),
		ObjectType: req.ObjectType,
		Aggregator: agg,
	}
}

// handleSubscribeAggregation processes a subscribeAggregation request. On
// success the connection receives a "subscribed" message followed by an
// initial "aggregationChanged" carrying the snapshot computed at subscribe
// time. Subsequent change events incrementally update the running state.
func (h *Hub) handleSubscribeAggregation(c *Connection, raw json.RawMessage) Message {
	var req AggregateSubscribeRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return Message{Type: "error", Error: "invalid subscribeAggregation request: " + err.Error()}
	}
	if req.ObjectType == "" {
		return Message{Type: "error", Error: "objectType is required"}
	}
	if err := validateAggMetric(req.Metric); err != nil {
		return Message{Type: "error", Error: err.Error()}
	}

	agg := newIncrementalAggregator(req.ObjectType, req.Where, req.Metric, req.GroupBy)
	if resolver := h.indexResolver(); resolver != nil {
		idx := resolver.GetIndex(req.ObjectType)
		if err := agg.Seed(idx, h.config.AggregationScanLimit); err != nil {
			return Message{Type: "error", Error: err.Error()}
		}
	}

	// Lock order: hub.mu → conn.subMu so the routing index update and
	// HandleObjectChange dispatch agree on lock acquisition order. Both
	// indexResolver() and the seed call ran before this so we don't re-enter
	// h.mu.
	h.mu.Lock()
	c.subMu.Lock()
	if len(c.subscriptions) >= MaxSubscriptionsPerConnection {
		c.subMu.Unlock()
		h.mu.Unlock()
		return Message{
			Type:  "error",
			Error: "maximum subscriptions per connection reached (10)",
		}
	}
	sub := newAggregationSubscription(req, agg)
	c.subscriptions[sub.ID] = sub
	h.addToIndexLocked(c, sub)
	c.subMu.Unlock()
	h.mu.Unlock()

	// Push the subscribed reply BEFORE the initial snapshot so clients see
	// "subscribed → aggregationChanged" in order. Returning Message{} signals
	// the readPump to skip the default dispatch — see hub.go.
	subscribed := Message{Type: "subscribed", SubscriptionID: sub.ID}
	select {
	case c.send <- subscribed:
	default:
	}
	sendAggregationChanged(c, sub.ID, agg.Snapshot())

	return Message{}
}

// sendAggregationChanged serialises the current aggregator state and pushes it
// to the connection's send channel. A full send buffer drops the message
// rather than blocking — overflow is signalled to the client via the existing
// onOutOfDate path next time the buffer drains.
func sendAggregationChanged(c *Connection, subID string, state *aggregation.AggregationResponse) {
	payload, err := json.Marshal(AggregationChangedPayload{State: state})
	if err != nil {
		return
	}
	msg := Message{
		Type:           "aggregationChanged",
		SubscriptionID: subID,
		Data:           payload,
	}
	select {
	case c.send <- msg:
	default:
		c.markOverflow(subID)
	}
}
