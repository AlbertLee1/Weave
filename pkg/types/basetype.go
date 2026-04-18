package types

import "encoding/json"

// BaseType represents a Palantir Ontology base type.
type BaseType string

const (
	String         BaseType = "string"
	Integer        BaseType = "integer"
	Short          BaseType = "short"
	Long           BaseType = "long"
	Float          BaseType = "float"
	Double         BaseType = "double"
	Boolean        BaseType = "boolean"
	Byte           BaseType = "byte"
	Date           BaseType = "date"
	Timestamp      BaseType = "timestamp"
	Decimal        BaseType = "decimal"
	Array          BaseType = "array"
	Struct         BaseType = "struct"
	Vector         BaseType = "vector"
	Geopoint       BaseType = "geopoint"
	Geoshape       BaseType = "geoshape"
	Attachment     BaseType = "attachment"
	TimeSeries     BaseType = "timeseries"
	MediaReference BaseType = "mediaReference"
	Media          BaseType = "media"
	Marking        BaseType = "marking"
	Cipher         BaseType = "cipher"
	Union          BaseType = "union"
)

// UnionDiscriminatorKey is the JSON object key that tags a union-typed value
// with the BaseType of the variant it matches.
const UnionDiscriminatorKey = "__type"

// UnionValueKey is the JSON object key that carries the wrapped inner value
// when a union-typed value is serialized with an explicit discriminator.
const UnionValueKey = "value"

// allBaseTypes is the authoritative set of valid base types.
var allBaseTypes = map[BaseType]bool{
	String: true, Integer: true, Short: true, Long: true,
	Float: true, Double: true, Boolean: true, Byte: true,
	Date: true, Timestamp: true, Decimal: true, Array: true,
	Struct: true, Vector: true, Geopoint: true, Geoshape: true,
	Attachment: true, TimeSeries: true, MediaReference: true,
	Media: true, Marking: true, Cipher: true, Union: true,
}

// CanBePrimaryKey reports whether the base type is eligible as a primary key.
func (bt BaseType) CanBePrimaryKey() bool {
	switch bt {
	case String, Integer, Long:
		return true
	default:
		return false
	}
}

// CanBeTitle reports whether the base type is eligible as a title property.
func (bt BaseType) CanBeTitle() bool {
	switch bt {
	case String, Integer, Long, Boolean:
		return true
	default:
		return false
	}
}

// IsValid reports whether the base type is one of the 22 known types.
func (bt BaseType) IsValid() bool {
	return allBaseTypes[bt]
}

// DataType describes a concrete Palantir data type, including parameterised
// types such as Array(subType), Struct(fields), and Union(variants).
type DataType struct {
	Type      BaseType            `json:"type"`
	SubType   *DataType           `json:"subType,omitempty"`
	Fields    map[string]DataType `json:"fields,omitempty"`
	Precision *int                `json:"precision,omitempty"`
	Scale     *int                `json:"scale,omitempty"`
	// Variants is populated when Type is Union; each entry is a candidate shape
	// a value may take. Order is meaningful: Coerce and Validate try variants
	// in the declared order and accept the first match.
	Variants []DataType `json:"variants,omitempty"`
}

// MarshalJSON implements custom JSON marshalling for DataType.
// We use a helper struct to control omitempty behaviour precisely.
func (dt DataType) MarshalJSON() ([]byte, error) {
	type alias DataType
	return json.Marshal(alias(dt))
}

// UnmarshalJSON implements custom JSON unmarshalling for DataType.
func (dt *DataType) UnmarshalJSON(data []byte) error {
	type alias DataType
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*dt = DataType(a)
	return nil
}
