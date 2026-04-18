package types

import (
	"fmt"
	"math"
	"strconv"
	"time"
)

// Coerce attempts to convert value to the canonical Go type for the given
// DataType. It returns the coerced value or an error if conversion is not
// possible.
func Coerce(value interface{}, dataType DataType) (interface{}, error) {
	if value == nil {
		return nil, nil
	}

	switch dataType.Type {
	case String:
		return coerceString(value)
	case Integer:
		return coerceInteger(value)
	case Short:
		return coerceShort(value)
	case Long:
		return coerceLong(value)
	case Float, Double:
		return coerceFloat(value)
	case Boolean:
		return coerceBoolean(value)
	case Date:
		return coerceDate(value)
	case Timestamp:
		return coerceTimestamp(value)
	case Array:
		return coerceArray(value, dataType)
	case Struct:
		return coerceStruct(value, dataType)
	case Union:
		return coerceUnion(value, dataType)
	default:
		return value, nil
	}
}

// coerceUnion picks the first variant that Coerce+Validate accepts and returns
// a tagged map {"__type": "<variant>", "value": <coerced>}. When the input is
// already tagged (carries __type), the named variant is used directly instead
// of probing every variant.
func coerceUnion(value interface{}, dataType DataType) (interface{}, error) {
	if len(dataType.Variants) == 0 {
		return nil, fmt.Errorf("union has no variants declared")
	}
	variantType, inner, tagged := unwrapUnionValue(value)
	if tagged {
		for _, v := range dataType.Variants {
			if v.Type != variantType {
				continue
			}
			coerced, err := Coerce(inner, v)
			if err != nil {
				return nil, fmt.Errorf("union variant %q: %w", variantType, err)
			}
			return wrapUnionValue(v.Type, coerced), nil
		}
		return nil, fmt.Errorf("union discriminator %q does not match any variant", variantType)
	}
	var lastErr error
	for _, v := range dataType.Variants {
		coerced, err := Coerce(value, v)
		if err != nil {
			lastErr = err
			continue
		}
		if err := Validate(coerced, v, false); err != nil {
			lastErr = err
			continue
		}
		return wrapUnionValue(v.Type, coerced), nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("value does not match any union variant: %w", lastErr)
	}
	return nil, fmt.Errorf("value does not match any union variant")
}

func wrapUnionValue(variant BaseType, inner interface{}) map[string]interface{} {
	return map[string]interface{}{
		UnionDiscriminatorKey: string(variant),
		UnionValueKey:         inner,
	}
}

func coerceString(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

func coerceInteger(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case int:
		return int32(v), nil
	case int32:
		return v, nil
	case int64:
		return int32(v), nil
	case float64:
		if v != math.Trunc(v) {
			return nil, fmt.Errorf("cannot coerce %v to integer: has fractional part", v)
		}
		return int32(v), nil
	case string:
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("cannot coerce %q to integer: %w", v, err)
		}
		return int32(n), nil
	default:
		return nil, fmt.Errorf("cannot coerce %T to integer", value)
	}
}

func coerceShort(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case int:
		return int16(v), nil
	case int16:
		return v, nil
	case int32:
		return int16(v), nil
	case float64:
		if v != math.Trunc(v) {
			return nil, fmt.Errorf("cannot coerce %v to short: has fractional part", v)
		}
		return int16(v), nil
	default:
		return nil, fmt.Errorf("cannot coerce %T to short", value)
	}
}

func coerceLong(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case float64:
		if v != math.Trunc(v) {
			return nil, fmt.Errorf("cannot coerce %v to long: has fractional part", v)
		}
		return int64(v), nil
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot coerce %q to long: %w", v, err)
		}
		return n, nil
	default:
		return nil, fmt.Errorf("cannot coerce %T to long", value)
	}
}

func coerceFloat(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot coerce %q to float: %w", v, err)
		}
		return n, nil
	default:
		return nil, fmt.Errorf("cannot coerce %T to float", value)
	}
}

func coerceBoolean(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	default:
		return nil, fmt.Errorf("cannot coerce %T to boolean", value)
	}
}

func coerceDate(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return nil, fmt.Errorf("cannot coerce %q to date: %w", v, err)
		}
		return t, nil
	case time.Time:
		return v, nil
	default:
		return nil, fmt.Errorf("cannot coerce %T to date", value)
	}
}

func coerceTimestamp(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, fmt.Errorf("cannot coerce %q to timestamp: %w", v, err)
		}
		return t, nil
	case time.Time:
		return v, nil
	default:
		return nil, fmt.Errorf("cannot coerce %T to timestamp", value)
	}
}

func coerceArray(value interface{}, dataType DataType) (interface{}, error) {
	arr, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("cannot coerce %T to array", value)
	}
	if dataType.SubType == nil {
		return arr, nil
	}
	result := make([]interface{}, len(arr))
	for i, elem := range arr {
		coerced, err := Coerce(elem, *dataType.SubType)
		if err != nil {
			return nil, fmt.Errorf("array element [%d]: %w", i, err)
		}
		result[i] = coerced
	}
	return result, nil
}

func coerceStruct(value interface{}, dataType DataType) (interface{}, error) {
	m, ok := value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("cannot coerce %T to struct", value)
	}
	if dataType.Fields == nil {
		return m, nil
	}
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		fieldType, hasField := dataType.Fields[k]
		if !hasField {
			// Pass through unknown fields
			result[k] = v
			continue
		}
		coerced, err := Coerce(v, fieldType)
		if err != nil {
			return nil, fmt.Errorf("struct field %q: %w", k, err)
		}
		result[k] = coerced
	}
	return result, nil
}
