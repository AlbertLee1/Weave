package pipeline

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrSchemaBreakingChange is the sentinel returned by ResolveSchemaEvolution
// when the diff between a pipeline's last-known schema and the freshly
// observed schema would silently drop or alter a column. APPEND-mode runs
// abort the run when this fires; the HTTP layer translates the wrap into
// the WEAVE_PIPELINE_BREAKING_CHANGE 422 envelope.
var ErrSchemaBreakingChange = errors.New("pipeline: schema evolution breaking change")

// SchemaDiff is the structured outcome of comparing two schemas.
// AddedColumns, DroppedColumns and ConflictedColumns are sorted
// ascending by name so the resulting wire payload is deterministic
// across runs (cheap change detection, stable test fixtures).
type SchemaDiff struct {
	// AddedColumns is the set of columns present in Current that were
	// absent from Prior. APPEND mode auto-adds these to the downstream
	// index without operator intervention.
	AddedColumns []SchemaField

	// DroppedColumns is the set of columns present in Prior that are
	// absent from Current. APPEND mode REJECTS these — silently dropping
	// a column from the index would lose downstream queryability without
	// any audit trail.
	DroppedColumns []SchemaField

	// ConflictedColumns is the set of columns whose type changed
	// between Prior and Current. Each entry surfaces the new field
	// shape; the mismatched prior type is in MismatchedPriorTypes
	// keyed by name. APPEND mode rejects these for the same reason as
	// drops — a silent type rewrite would corrupt downstream values.
	ConflictedColumns []SchemaField

	// MismatchedPriorTypes records `name → priorType` for every entry
	// in ConflictedColumns so callers can render a `name: oldType→newType`
	// diff without a second walk.
	MismatchedPriorTypes map[string]string
}

// IsBreaking reports whether the diff contains any change that requires
// operator intervention (drops or type conflicts). Pure additions are
// non-breaking.
func (d SchemaDiff) IsBreaking() bool {
	return len(d.DroppedColumns) > 0 || len(d.ConflictedColumns) > 0
}

// DiffSchemas compares prior and current schemas and returns the
// structured diff. Either side may be nil — a nil prior is treated as
// "no schema known yet" and yields every current column as an addition.
//
// Column comparisons are case-sensitive on Name. Type comparisons are
// trimmed + case-insensitive (so "STRING" / "string" / " String " are
// equivalent) — schema sources frequently disagree on casing without
// any semantic difference.
func DiffSchemas(prior, current []SchemaField) SchemaDiff {
	priorByName := make(map[string]SchemaField, len(prior))
	for _, f := range prior {
		priorByName[f.Name] = f
	}
	currentByName := make(map[string]SchemaField, len(current))
	for _, f := range current {
		currentByName[f.Name] = f
	}
	diff := SchemaDiff{
		MismatchedPriorTypes: map[string]string{},
	}
	// Walk current to find adds + conflicts.
	for _, c := range current {
		p, ok := priorByName[c.Name]
		if !ok {
			diff.AddedColumns = append(diff.AddedColumns, c)
			continue
		}
		if !typesEquivalent(p.Type, c.Type) {
			diff.ConflictedColumns = append(diff.ConflictedColumns, c)
			diff.MismatchedPriorTypes[c.Name] = p.Type
		}
	}
	// Walk prior to find drops.
	for _, p := range prior {
		if _, ok := currentByName[p.Name]; !ok {
			diff.DroppedColumns = append(diff.DroppedColumns, p)
		}
	}
	sortSchemaFields(diff.AddedColumns)
	sortSchemaFields(diff.DroppedColumns)
	sortSchemaFields(diff.ConflictedColumns)
	return diff
}

// MergeSchema combines prior and current into the canonical post-run
// schema:
//
//   - every column in prior is preserved (no in-place type rewrites)
//   - every new column in current is appended in the order it appears
//
// The merge is order-preserving relative to prior so downstream
// indexes that ride on the field ordering (Bleve facet position, e.g.)
// don't shuffle on every run.
func MergeSchema(prior, current []SchemaField) []SchemaField {
	if len(prior) == 0 {
		out := make([]SchemaField, len(current))
		copy(out, current)
		return out
	}
	priorByName := make(map[string]struct{}, len(prior))
	out := make([]SchemaField, 0, len(prior)+len(current))
	for _, p := range prior {
		out = append(out, p)
		priorByName[p.Name] = struct{}{}
	}
	for _, c := range current {
		if _, ok := priorByName[c.Name]; ok {
			continue
		}
		out = append(out, c)
	}
	return out
}

// SchemaEvolutionResolution is what ResolveSchemaEvolution returns on
// the success path: the new canonical schema (prior ∪ current additions)
// AND the list of column NAMES that were freshly added. Callers funnel
// AddedColumns into the downstream-index "extend" hook and persist
// MergedSchema as the new pipelines.last_known_schema.
type SchemaEvolutionResolution struct {
	MergedSchema []SchemaField
	AddedColumns []string
	Diff         SchemaDiff
}

// ResolveSchemaEvolution applies the US-378 schema-evolution rules:
//
//   - new columns auto-add (returned as AddedColumns + folded into
//     MergedSchema)
//   - dropped columns produce ErrSchemaBreakingChange
//   - type-conflicting columns produce ErrSchemaBreakingChange
//
// Both error shapes wrap ErrSchemaBreakingChange so callers can route
// with a single errors.Is check.
func ResolveSchemaEvolution(prior, current []SchemaField) (SchemaEvolutionResolution, error) {
	diff := DiffSchemas(prior, current)
	if diff.IsBreaking() {
		var parts []string
		if len(diff.DroppedColumns) > 0 {
			names := make([]string, len(diff.DroppedColumns))
			for i, f := range diff.DroppedColumns {
				names[i] = f.Name
			}
			parts = append(parts, fmt.Sprintf("dropped columns: %s", strings.Join(names, ", ")))
		}
		if len(diff.ConflictedColumns) > 0 {
			names := make([]string, len(diff.ConflictedColumns))
			for i, f := range diff.ConflictedColumns {
				names[i] = fmt.Sprintf("%s (%s→%s)", f.Name, diff.MismatchedPriorTypes[f.Name], f.Type)
			}
			parts = append(parts, fmt.Sprintf("type conflicts: %s", strings.Join(names, ", ")))
		}
		return SchemaEvolutionResolution{Diff: diff}, fmt.Errorf("%w: %s", ErrSchemaBreakingChange, strings.Join(parts, "; "))
	}
	added := make([]string, 0, len(diff.AddedColumns))
	for _, f := range diff.AddedColumns {
		added = append(added, f.Name)
	}
	return SchemaEvolutionResolution{
		MergedSchema: MergeSchema(prior, current),
		AddedColumns: added,
		Diff:         diff,
	}, nil
}

// typesEquivalent compares two schema-field type strings under
// the trim+case-insensitive equivalence relation.
func typesEquivalent(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// sortSchemaFields sorts in place by Name ascending. Stable so adjacent
// fields with the same name (impossible by validation, but defensively
// handled) keep their input order.
func sortSchemaFields(fs []SchemaField) {
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Name < fs[j].Name })
}
