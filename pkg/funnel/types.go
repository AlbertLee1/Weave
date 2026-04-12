package funnel

import "time"

// EditType represents the type of edit operation.
type EditType string

const (
	EditTypeCreate EditType = "CREATE"
	EditTypeModify EditType = "MODIFY"
	EditTypeDelete EditType = "DELETE"
)

// Edit source discriminators used by the user-edit-wins conflict resolver in
// the funnel consumer. Action-executor writes carry SourceUser so subsequent
// ingest rewrites cannot silently overwrite them; streaming ingest paths must
// set SourceIngest explicitly so the consumer can compare timestamps against
// the most recent user edit. An empty string is treated as SourceUser by
// downstream code so legacy callers and replayed batches from before US-020
// keep their existing semantics.
const (
	EditSourceUser   = "user"
	EditSourceIngest = "ingest"
)

// Edit represents a single object edit.
//
// Source is the US-020 discriminator used by the funnel consumer's user-edit
// -wins conflict logic (US-021). Action-executor edits are stamped "user"; a
// future streaming ingest path will stamp "ingest". It is omitempty so the
// on-the-wire shape stays backwards compatible with pre-US-020 consumers.
//
// Markings is the US-051 side-channel used to ship per-object marking sets
// into the Bleve index. Values land under the reserved keyword field
// security.MarkingField (a.k.a. "_markings") on the indexed document so the
// policy engine's auto-marking clause can AND-combine a TermQuery against
// the same field. omitempty keeps the wire shape backwards compatible with
// pre-US-051 publishers; an empty slice is treated identically to "no
// markings" and no key is written into the indexed doc.
type Edit struct {
	Type       EditType               `json:"type"`
	ObjectType string                 `json:"objectType"`
	PrimaryKey string                 `json:"primaryKey"`
	Properties map[string]interface{} `json:"properties,omitempty"` // for CREATE/MODIFY
	Source     string                 `json:"source,omitempty"`
	Markings   []string               `json:"markings,omitempty"`
}

// EditBatch represents a batch of edits to be applied atomically.
//
// US-044: OntologyAPIName scopes the entire batch to one ontology so the
// publisher routes it onto a per-ontology subject and the consumer applies
// it to the per-ontology Bleve index. All Edits in a batch must belong to
// this ontology; cross-ontology mutations require separate batches.
type EditBatch struct {
	ID              string    `json:"id"`
	OntologyAPIName string    `json:"ontologyApiName"`
	Edits           []Edit    `json:"edits"`
	UserID          string    `json:"userId"`
	Timestamp       time.Time `json:"timestamp"`
}

// ChangeEvent is emitted after edits are applied to notify subscribers.
type ChangeEvent struct {
	ObjectType string   `json:"objectType"`
	PrimaryKey string   `json:"primaryKey"`
	EditType   EditType `json:"editType"`
	Offset     uint64   `json:"offset"` // NATS sequence number
}
