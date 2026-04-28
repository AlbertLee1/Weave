// Package cdc implements a Change Data Capture receiver that consumes
// PostgreSQL logical replication output and converts row-level changes
// into funnel.EditBatches the rest of the system already understands.
//
// The package is layered so the wire decoding (pgoutput via
// jackc/pglogrepl), the runtime orchestration (Receiver), the
// transport (Source / Publisher interfaces), and the schema mapping
// (TableMapping) are independently testable. Production wiring lives
// in cmd/server which composes a real PG-backed Source with a funnel
// Publisher; tests use in-memory implementations.
package cdc

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pglogrepl"
)

// ChangeOp identifies the row-level operation a ChangeEvent represents.
type ChangeOp string

const (
	// ChangeOpInsert is a row insertion.
	ChangeOpInsert ChangeOp = "INSERT"
	// ChangeOpUpdate is a row update.
	ChangeOpUpdate ChangeOp = "UPDATE"
	// ChangeOpDelete is a row deletion.
	ChangeOpDelete ChangeOp = "DELETE"
)

// ChangeEvent is one row-level change emitted by the decoder.
//
// Before is populated for UPDATE (when the table's REPLICA IDENTITY is
// FULL or carries a key tuple) and DELETE; otherwise nil. After is
// populated for INSERT and UPDATE; nil for DELETE. Values are stored
// as strings because pgoutput emits text-format tuples by default; the
// mapper coerces them into typed property values per TableMapping.
type ChangeEvent struct {
	Op         ChangeOp          `json:"op"`
	Schema     string            `json:"schema"`
	Table      string            `json:"table"`
	RelationID uint32            `json:"relationId"`
	Before     map[string]string `json:"before,omitempty"`
	After      map[string]string `json:"after,omitempty"`
	LSN        pglogrepl.LSN     `json:"lsn"`
	CommitLSN  pglogrepl.LSN     `json:"commitLsn"`
	CommitTime time.Time         `json:"commitTime"`
}

// TableMapping declares how rows from a single source table become
// funnel.Edits on a target ObjectType.
//
//	Schema/Table         — match the pgoutput Relation message; comparison
//	                       is case-sensitive (PostgreSQL identifiers).
//	OntologyAPIName      — destination ontology; rides on EditBatch.
//	ObjectType           — target ObjectType API name.
//	PrimaryKeyColumns    — ordered list of source columns whose text
//	                       values concatenate (with ":" separator) into
//	                       the funnel.Edit.PrimaryKey. Single-column PKs
//	                       are emitted unchanged; composite keys mirror
//	                       oms.ParseCompositeKey's separator.
//	PropertyColumns      — map source column → property API name. Columns
//	                       not listed are dropped silently. Empty map means
//	                       "no scalar properties propagated" (a key-only
//	                       sync, useful for delete-only mirrors).
//	IncludeNullProperties — when false (default) NULL property values are
//	                       omitted from the resulting Edit; when true they
//	                       are passed through as nil so MODIFY edits can
//	                       explicitly clear fields. INSERT edits ignore
//	                       this flag because absent keys equal "no value".
type TableMapping struct {
	Schema                string
	Table                 string
	OntologyAPIName       string
	ObjectType            string
	PrimaryKeyColumns     []string
	PropertyColumns       map[string]string
	IncludeNullProperties bool
}

// Validate rejects mappings that would silently produce malformed
// edits. Called by Config.Validate; safe to call directly when admin
// surfaces want to surface a 400 before persisting.
func (m TableMapping) Validate() error {
	if strings.TrimSpace(m.Schema) == "" {
		return errors.New("cdc: TableMapping.Schema must not be empty")
	}
	if strings.TrimSpace(m.Table) == "" {
		return errors.New("cdc: TableMapping.Table must not be empty")
	}
	if strings.TrimSpace(m.OntologyAPIName) == "" {
		return errors.New("cdc: TableMapping.OntologyAPIName must not be empty")
	}
	if strings.TrimSpace(m.ObjectType) == "" {
		return errors.New("cdc: TableMapping.ObjectType must not be empty")
	}
	if len(m.PrimaryKeyColumns) == 0 {
		return errors.New("cdc: TableMapping.PrimaryKeyColumns must declare at least one column")
	}
	for i, c := range m.PrimaryKeyColumns {
		if strings.TrimSpace(c) == "" {
			return fmt.Errorf("cdc: TableMapping.PrimaryKeyColumns[%d] must not be empty", i)
		}
	}
	for col, prop := range m.PropertyColumns {
		if strings.TrimSpace(col) == "" {
			return errors.New("cdc: TableMapping.PropertyColumns has an empty column key")
		}
		if strings.TrimSpace(prop) == "" {
			return fmt.Errorf("cdc: TableMapping.PropertyColumns[%q] target name must not be empty", col)
		}
	}
	return nil
}

// Key returns the canonical (schema, table) lookup key the Config
// uses to resolve incoming ChangeEvents to a mapping.
func (m TableMapping) Key() string {
	return m.Schema + "." + m.Table
}

// Config is the full CDC routing table. A single Config typically
// covers many tables; the Receiver consults it on every event.
type Config struct {
	Tables []TableMapping
}

// Validate reports the first invalid mapping in the config; mappings
// must also have unique (Schema, Table) keys.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("cdc: Config is nil")
	}
	seen := make(map[string]struct{}, len(c.Tables))
	for i, m := range c.Tables {
		if err := m.Validate(); err != nil {
			return fmt.Errorf("Tables[%d]: %w", i, err)
		}
		key := m.Key()
		if _, dup := seen[key]; dup {
			return fmt.Errorf("Tables[%d]: duplicate mapping for %s", i, key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// Lookup returns the mapping for (schema, table) or (nil, false) if no
// mapping is configured for that table. Cheap O(N) scan — the
// expected mapping count is < 100, well below the threshold where a
// hash map would be worth the construction cost.
func (c *Config) Lookup(schema, table string) (TableMapping, bool) {
	if c == nil {
		return TableMapping{}, false
	}
	for _, m := range c.Tables {
		if m.Schema == schema && m.Table == table {
			return m, true
		}
	}
	return TableMapping{}, false
}

// CompositeKeySeparator joins the text values of multi-column primary
// keys into one funnel.Edit.PrimaryKey string. Mirrors
// oms.CompositeKeySeparator so chi composite-key URL routing
// (oms.ParseCompositeKey) can round-trip the value back into its
// component columns when needed.
const CompositeKeySeparator = ":"

// PrimaryKeyFor concatenates the source columns into the primary key
// string the Edit will carry. Missing columns produce an error so the
// caller can surface the misconfiguration; absent values would
// otherwise silently route to the wrong primary key.
func (m TableMapping) PrimaryKeyFor(values map[string]string) (string, error) {
	if len(m.PrimaryKeyColumns) == 0 {
		return "", errors.New("cdc: mapping declares no primary-key columns")
	}
	parts := make([]string, len(m.PrimaryKeyColumns))
	for i, col := range m.PrimaryKeyColumns {
		v, ok := values[col]
		if !ok {
			return "", fmt.Errorf("cdc: primary-key column %q missing from change event", col)
		}
		parts[i] = v
	}
	return strings.Join(parts, CompositeKeySeparator), nil
}
