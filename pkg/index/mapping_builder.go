package index

import (
	"encoding/json"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/liyang/weave/pkg/oms"
)

// Analyzer names recognised by BuildMapping via a property's TypeConfig
// `analyzer` field. These mirror the Foundry OSv2 property typeclass
// convention (analyzer.not_analyzed / analyzer.standard / analyzer.not_indexed).
const (
	AnalyzerNotAnalyzed = "not_analyzed"
	AnalyzerStandard    = "standard"
	AnalyzerNotIndexed  = "not_indexed"
)

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

	im.DefaultMapping = dm
	return im
}

// buildFieldMapping converts a single oms.Property into a Bleve FieldMapping.
// Returns nil when the property should be skipped entirely.
func buildFieldMapping(p oms.Property) *mapping.FieldMapping {
	analyzer := propertyAnalyzer(p)

	if analyzer == AnalyzerNotIndexed {
		fm := mapping.NewTextFieldMapping()
		fm.Index = false
		fm.Store = true
		return fm
	}

	if !p.IsSearchable {
		fm := mapping.NewTextFieldMapping()
		fm.Index = false
		fm.Store = true
		return fm
	}

	if isTextBaseType(p.BaseType) {
		if analyzer == AnalyzerNotAnalyzed {
			return mapping.NewKeywordFieldMapping()
		}
		return mapping.NewTextFieldMapping()
	}

	switch p.BaseType {
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
