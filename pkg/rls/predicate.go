package rls

import (
	"encoding/json"

	"github.com/liyang/weave/pkg/oss/where"
)

// unmarshalPredicate decodes a stored JSONB predicate into a WhereClause.
// Kept as a named helper so the JSON tag set is documented in one place —
// where.WhereClause already has the required tags so this is a thin wrapper.
func unmarshalPredicate(raw json.RawMessage, out *where.WhereClause) error {
	return json.Unmarshal(raw, out)
}
