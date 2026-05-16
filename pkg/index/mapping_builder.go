package index

import (
	"encoding/json"

	"github.com/blevesearch/bleve/v2"
	// Register the CJK analyzer so fieldMappingFor can select it for CJK
	// (Chinese / Japanese / Korean) text. Bleve's CJK analyzer applies a
	// width filter, lowercase, and a bigram filter on top of the unicode
	// tokenizer — bigram analysis is what lets "中国银行" match
	// "中国银行总部" (US-237).
	_ "github.com/blevesearch/bleve/v2/analysis/lang/cjk"
	// Register the English language analyzer so fieldMappingFor can select
	// it by name for standard-text fields. The Porter/Snowball stemmer this
	// package ships gives Foundry-like semantics for word-root matching
	// (e.g. "run" lights up "running" / "runs") — see US-012.
	_ "github.com/blevesearch/bleve/v2/analysis/lang/en"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/liyang/weave/pkg/oms"
)

// standardTextAnalyzer is the Bleve analyzer name used for default / standard
// text fields. We deliberately route to the English analyzer (lowercase +
// possessive strip + snowball stemmer) rather than bleve's "standard"
// analyzer so that root-form queries behave like Foundry's TypeClass=standard
// semantics. See US-012 for the acceptance contract.
const standardTextAnalyzer = "en"

// Analyzer names recognised by BuildMapping via a property's TypeConfig
// `analyzer` field. These mirror the Foundry OSv2 property typeclass
// convention (analyzer.not_analyzed / analyzer.standard / analyzer.not_indexed).
const (
	AnalyzerNotAnalyzed = "not_analyzed"
	AnalyzerStandard    = "standard"
	AnalyzerNotIndexed  = "not_indexed"
	// AnalyzerCJK selects bleve's CJK analyzer (unicode tokenizer + width
	// filter + lowercase + bigram filter). Use it on text fields holding
	// Chinese / Japanese / Korean content so that token-style queries like
	// "中国银行" match "中国银行总部" via shared bigrams. US-237.
	AnalyzerCJK = "cjk"
	// AnalyzerEnglish is the explicit Foundry-style hint for the English
	// language analyzer (lowercase + possessive strip + Porter/Snowball
	// stemmer). Equivalent to AnalyzerStandard / unset — Foundry callers who
	// spell it as `english` in TypeConfig get the same mapping. US-461.
	AnalyzerEnglish = "english"
)

// MarkingsField is the reserved Bleve keyword field that carries every
// object's marking set. Kept in lockstep with pkg/security.MarkingField
// (the policy engine's auto-marking clause) and pkg/funnel.markingsField
// (the ingest-side writer). Declared here so every ObjectType mapping can
// register a KeywordField for markings without going through the schema.
const MarkingsField = "_markings"

// BuildMapping constructs a Bleve IndexMapping from an oms.ObjectType,
// honouring each property's `TypeConfig.analyzer` hint:
//
//   - not_analyzed → KeywordField (case-sensitive exact match)
//   - standard or unset → TextField (standard analyzer, tokenised + stemmed)
//   - not_indexed → stored but Index=false (returned by API, never searched)
//
// Non-text base types (numeric / boolean / date / geopoint) use their
// dedicated Bleve field mapping regardless of analyzer hint. US-010 adds
// the not_indexed enforcement; this entry point already respects it so the
// follow-up story only adds coverage.
func BuildMapping(ot *oms.ObjectType) *mapping.IndexMappingImpl {
	im := bleve.NewIndexMapping()
	dm := bleve.NewDocumentMapping()

	for _, p := range ot.Properties {
		fm := buildFieldMapping(p)
		if fm == nil {
			continue
		}
		dm.AddFieldMappingsAt(p.APIName, fm)
	}

	dm.AddFieldMappingsAt(MarkingsField, mapping.NewKeywordFieldMapping())

	im.DefaultMapping = dm
	return im
}

// buildFieldMapping converts a single oms.Property into a Bleve FieldMapping.
// Returns nil when the property should be skipped entirely.
func buildFieldMapping(p oms.Property) *mapping.FieldMapping {
	return fieldMappingFor(propertyAnalyzer(p), p.BaseType, p.IsSearchable)
}

// fieldMappingFor is the shared core used by BuildMapping (oms.Property path)
// and Manager.buildMapping (index.Property path). Centralising the switch
// here keeps the two entry points from drifting as new analyzer hints are
// added.
func fieldMappingFor(analyzer, baseType string, isSearchable bool) *mapping.FieldMapping {
	if analyzer == AnalyzerNotIndexed {
		fm := mapping.NewTextFieldMapping()
		fm.Index = false
		fm.Store = true
		return fm
	}

	if !isSearchable {
		// Use the type-appropriate field mapping so numeric/boolean/date
		// values round-trip through Bleve correctly. A TextFieldMapping
		// cannot store float64 values, so salary-like fields would be
		// silently dropped.
		var fm *mapping.FieldMapping
		switch baseType {
		case "integer", "short", "long", "float", "double", "byte":
			fm = mapping.NewNumericFieldMapping()
		case "boolean":
			fm = mapping.NewBooleanFieldMapping()
		case "date", "timestamp":
			fm = mapping.NewDateTimeFieldMapping()
		default:
			fm = mapping.NewTextFieldMapping()
		}
		fm.Index = false
		fm.Store = true
		return fm
	}

	if isTextBaseType(baseType) {
		if analyzer == AnalyzerNotAnalyzed {
			return mapping.NewKeywordFieldMapping()
		}
		fm := mapping.NewTextFieldMapping()
		switch analyzer {
		case AnalyzerCJK:
			fm.Analyzer = AnalyzerCJK
		case AnalyzerEnglish, AnalyzerStandard, "":
			fm.Analyzer = standardTextAnalyzer
		default:
			fm.Analyzer = standardTextAnalyzer
		}
		return fm
	}

	switch baseType {
	case "integer", "short", "long", "float", "double", "byte":
		return mapping.NewNumericFieldMapping()
	case "boolean":
		return mapping.NewBooleanFieldMapping()
	case "date", "timestamp":
		return mapping.NewDateTimeFieldMapping()
	case "geopoint":
		return mapping.NewGeoPointFieldMapping()
	}

	return mapping.NewTextFieldMapping()
}

// AnalyzerFromTypeConfig extracts the `analyzer` hint from a property's
// TypeConfig JSON blob. It returns an empty string for nil / malformed input
// so callers can treat "no hint" the same as "default text".
//
// This is the canonical helper used by the rehydrate bootstrap (which only
// has oms.Property in hand) to populate index.Property.Analyzer.
func AnalyzerFromTypeConfig(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var cfg struct {
		Analyzer string `json:"analyzer"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return ""
	}
	return cfg.Analyzer
}

// propertyAnalyzer extracts the analyzer hint from p.TypeConfig if present.
// Returns the empty string when no hint is set, leaving the caller to apply
// its default (standard text analyzer).
func propertyAnalyzer(p oms.Property) string {
	if len(p.TypeConfig) == 0 {
		return ""
	}
	var cfg struct {
		Analyzer string `json:"analyzer"`
	}
	if err := json.Unmarshal(p.TypeConfig, &cfg); err != nil {
		return ""
	}
	return cfg.Analyzer
}

func isTextBaseType(t string) bool {
	switch t {
	case "string", "":
		return true
	}
	return false
}
