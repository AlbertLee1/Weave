package types

import (
	"fmt"
	"strconv"
	"time"
)

// Validate checks whether value is acceptable for the given DataType.
// If nullable is true, nil values are permitted regardless of type.
func Validate(value interface{}, dataType DataType, nullable bool) error {
	if value == nil {
		if nullable {
			return nil
		}
		return fmt.Errorf("value is nil but field is not nullable")
	}

	switch dataType.Type {
	case String:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string, got %T", value)
		}

	case Integer:
		switch value.(type) {
		case int, int32, int64, float64:
			// accepted numeric types
		default:
			return fmt.Errorf("expected integer, got %T", value)
		}

	case Short:
		switch value.(type) {
		case int, int16, int32, float64:
		default:
			return fmt.Errorf("expected short, got %T", value)
		}

	case Long:
		switch v := value.(type) {
		case int, int32, int64, float64:
			_ = v
		case string:
			// JS precision compatibility: accept numeric strings
			if _, err := strconv.ParseInt(v, 10, 64); err != nil {
				return fmt.Errorf("expected long (as string), got %q: %w", v, err)
			}
		default:
			return fmt.Errorf("expected long, got %T", value)
		}

	case Float, Double:
		switch value.(type) {
		case float32, float64, int, int32, int64:
		default:
			return fmt.Errorf("expected %s, got %T", dataType.Type, value)
		}

	case Boolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean, got %T", value)
		}

	case Byte:
		switch value.(type) {
		case int, int8, byte, float64:
		default:
			return fmt.Errorf("expected byte, got %T", value)
		}

	case Date:
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected date string, got %T", value)
		}
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return fmt.Errorf("invalid date format %q: %w", s, err)
		}

	case Timestamp:
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected timestamp string, got %T", value)
		}
		if _, err := time.Parse(time.RFC3339, s); err != nil {
			return fmt.Errorf("invalid timestamp format %q: %w", s, err)
		}

	case Decimal:
		switch value.(type) {
		case float32, float64, int, int32, int64, string:
		default:
			return fmt.Errorf("expected decimal, got %T", value)
		}

	case Array:
		if _, ok := value.([]interface{}); !ok {
			return fmt.Errorf("expected array, got %T", value)
		}

	case Struct:
		if _, ok := value.(map[string]interface{}); !ok {
			return fmt.Errorf("expected struct (map), got %T", value)
		}

	case Union:
		return validateUnion(value, dataType, nullable)

	default:
		// For other types (vector, geopoint, geoshape, etc.) accept any value.
	}

	return nil
}

// validateUnion accepts value iff it matches at least one declared variant.
// If value is a map carrying a "__type" discriminator, only the named variant
// is considered. Otherwise variants are tried in declared order.
func validateUnion(value interface{}, dataType DataType, nullable bool) error {
	if len(dataType.Variants) == 0 {
		return fmt.Errorf("union has no variants declared")
	}
	variantType, inner, tagged := unwrapUnionValue(value)
	if tagged {
		for _, v := range dataType.Variants {
			if v.Type == variantType {
				return Validate(inner, v, nullable)
			}
		}
		return fmt.Errorf("union discriminator %q does not match any variant", variantType)
	}
	for _, v := range dataType.Variants {
		if err := Validate(value, v, nullable); err == nil {
			return nil
		}
	}
	return fmt.Errorf("value %T does not match any union variant", value)
}

// unwrapUnionValue inspects value for a tagged-union wrapper.
// A tagged value is a map that carries a string __type key; the inner value is
// either the map's "value" key (for scalar variants) or the map itself minus
// "__type" (for struct variants). Returns (variant BaseType, inner value, true)
// when tagged; otherwise (_, value, false).
func unwrapUnionValue(value interface{}) (BaseType, interface{}, bool) {
	m, ok := value.(map[string]interface{})
	if !ok {
		return "", value, false
	}
	raw, ok := m[UnionDiscriminatorKey]
	if !ok {
		return "", value, false
	}
	tag, ok := raw.(string)
	if !ok {
		return "", value, false
	}
	if inner, hasValue := m[UnionValueKey]; hasValue && len(m) == 2 {
		return BaseType(tag), inner, true
	}
	// Struct-style: strip __type and pass the remaining map through.
	stripped := make(map[string]interface{}, len(m)-1)
	for k, v := range m {
		if k == UnionDiscriminatorKey {
			continue
		}
		stripped[k] = v
	}
	return BaseType(tag), stripped, true
}
