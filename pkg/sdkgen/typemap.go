package sdkgen

import (
	"fmt"

	"github.com/liyang/weave/pkg/types"
)

// TypeMap maps BaseType constants to their target language type names.
type TypeMap map[types.BaseType]string

// TypeMapForLanguage returns the BaseType-to-language-type mapping for the
// specified language. Returns an error for unsupported languages.
func TypeMapForLanguage(lang string) (TypeMap, error) {
	switch lang {
	case "ts":
		return tsTypeMap, nil
	case "python":
		return pythonTypeMap, nil
	case "go":
		return goTypeMap, nil
	default:
		return nil, fmt.Errorf("unsupported language: %q", lang)
	}
}

var tsTypeMap = TypeMap{
	types.String:         "string",
	types.Integer:        "number",
	types.Short:          "number",
	types.Long:           "string",
	types.Float:          "number",
	types.Double:         "number",
	types.Boolean:        "boolean",
	types.Byte:           "number",
	types.Date:           "string",
	types.Timestamp:      "string",
	types.Decimal:        "string",
	types.Geopoint:       "{ lat: number; lon: number }",
	types.Geoshape:       "GeoJSON.Geometry",
	types.Attachment:     "string",
	types.TimeSeries:     "string",
	types.MediaReference: "string",
	types.Marking:        "string",
	types.Cipher:         "string",
}

var pythonTypeMap = TypeMap{
	types.String:         "str",
	types.Integer:        "int",
	types.Short:          "int",
	types.Long:           "int",
	types.Float:          "float",
	types.Double:         "float",
	types.Boolean:        "bool",
	types.Byte:           "int",
	types.Date:           "datetime.date",
	types.Timestamp:      "datetime.datetime",
	types.Decimal:        "Decimal",
	types.Geopoint:       "dict[str, float]",
	types.Geoshape:       "dict[str, Any]",
	types.Attachment:     "str",
	types.TimeSeries:     "str",
	types.MediaReference: "str",
	types.Marking:        "str",
	types.Cipher:         "str",
}

var goTypeMap = TypeMap{
	types.String:         "string",
	types.Integer:        "int32",
	types.Short:          "int16",
	types.Long:           "int64",
	types.Float:          "float32",
	types.Double:         "float64",
	types.Boolean:        "bool",
	types.Byte:           "byte",
	types.Date:           "string",
	types.Timestamp:      "time.Time",
	types.Decimal:        "string",
	types.Geopoint:       "GeoPoint",
	types.Geoshape:       "json.RawMessage",
	types.Attachment:     "string",
	types.TimeSeries:     "string",
	types.MediaReference: "string",
	types.Marking:        "string",
	types.Cipher:         "string",
}
