package types

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// Constraints defines the constraint fields that a ValueType can impose on
// property values. All fields are optional; only non-zero fields are enforced.
type Constraints struct {
	Regex     string        `json:"regex,omitempty"`
	MinLength *int          `json:"minLength,omitempty"`
	MaxLength *int          `json:"maxLength,omitempty"`
	Min       *float64      `json:"min,omitempty"`
	Max       *float64      `json:"max,omitempty"`
	Enum      []interface{} `json:"enum,omitempty"`
}

// ValidateConstraints checks value against the constraints encoded as JSON.
// A nil or empty constraints blob is a no-op. Nil values pass unconditionally
// because nullability is enforced by the existing Validate function.
func ValidateConstraints(value interface{}, constraints json.RawMessage) error {
	if len(constraints) == 0 {
		return nil
	}
	if value == nil {
		return nil
	}

	var c Constraints
	if err := json.Unmarshal(constraints, &c); err != nil {
		return fmt.Errorf("invalid constraints JSON: %w", err)
	}

	if c.Regex != "" {
		s, ok := value.(string)
		if ok {
			re, err := regexp.Compile(c.Regex)
			if err != nil {
				return fmt.Errorf("invalid regex constraint %q: %w", c.Regex, err)
			}
			if !re.MatchString(s) {
				return fmt.Errorf("regex: value %q does not match pattern %q", s, c.Regex)
			}
		}
	}

	if c.MinLength != nil {
		s, ok := value.(string)
		if ok && len(s) < *c.MinLength {
			return fmt.Errorf("minLength: value length %d is less than minimum %d", len(s), *c.MinLength)
		}
	}

	if c.MaxLength != nil {
		s, ok := value.(string)
		if ok && len(s) > *c.MaxLength {
			return fmt.Errorf("maxLength: value length %d exceeds maximum %d", len(s), *c.MaxLength)
		}
	}

	n, isNum := toFloat64(value)

	if c.Min != nil && isNum {
		if n < *c.Min {
			return fmt.Errorf("min: value %v is less than minimum %v", value, *c.Min)
		}
	}

	if c.Max != nil && isNum {
		if n > *c.Max {
			return fmt.Errorf("max: value %v exceeds maximum %v", value, *c.Max)
		}
	}

	if len(c.Enum) > 0 {
		found := false
		sv := fmt.Sprint(value)
		for _, allowed := range c.Enum {
			if fmt.Sprint(allowed) == sv {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("enum: value %v is not in allowed values %v", value, c.Enum)
		}
	}

	return nil
}

// toFloat64 converts numeric types to float64 for range comparison.
func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
