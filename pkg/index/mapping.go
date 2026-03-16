package index

import (
	"github.com/blevesearch/bleve/v2/mapping"
)

// FieldMappingForBaseType returns the appropriate Bleve field mapping for a given base type string.
func FieldMappingForBaseType(baseType string, isSearchable bool) *mapping.FieldMapping {
	if !isSearchable {
		fm := mapping.NewTextFieldMapping()
		fm.Index = false
		fm.Store = true
		return fm
	}

	switch baseType {
	case "string":
		return mapping.NewTextFieldMapping()
	case "integer", "short", "long", "float", "double", "byte":
		return mapping.NewNumericFieldMapping()
	case "boolean":
		return mapping.NewBooleanFieldMapping()
	case "date", "timestamp":
		return mapping.NewDateTimeFieldMapping()
	case "geopoint":
		return mapping.NewGeoPointFieldMapping()
	default:
		// For complex types (array, struct, decimal, etc.), store as text
		return mapping.NewTextFieldMapping()
	}
}
