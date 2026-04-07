package metrics

import "time"

// DBQuery records a single database operation. Call this with a logical
// operation name (e.g. "select_object_type", "insert_property"), the query
// error (or nil), and the elapsed time the call took. The label set is
// intentionally tiny — we partition by operation, NOT by SQL text — so
// the cardinality stays bounded.
func DBQuery(operation string, err error, duration time.Duration) {
	dbQueriesTotal.WithLabelValues(operation, statusLabel(err)).Inc()
	observeDuration(dbQueryDuration.WithLabelValues(operation), duration)
}

// BleveSearch records a single Bleve search invocation. Call this from the
// search code path with the object type api name, the search error (or
// nil), and the elapsed time spent in Bleve.
func BleveSearch(objectType string, err error, duration time.Duration) {
	bleveSearchTotal.WithLabelValues(objectType, statusLabel(err)).Inc()
	observeDuration(bleveSearchDuration.WithLabelValues(objectType), duration)
}

// SetBleveIndexDocs sets the gauge for the number of documents in the Bleve
// index for the given object type. Call this whenever the consumer applies
// a batch so the gauge tracks the latest count.
func SetBleveIndexDocs(objectType string, docs float64) {
	bleveIndexDocs.WithLabelValues(objectType).Set(docs)
}

// ActionApplied records a single Action execution. Call this with the
// action type api name, the action error (or nil), and the elapsed time
// the executor spent applying the action.
func ActionApplied(actionType string, err error, duration time.Duration) {
	actionsAppliedTotal.WithLabelValues(actionType, statusLabel(err)).Inc()
	observeDuration(actionsDuration.WithLabelValues(actionType), duration)
}
