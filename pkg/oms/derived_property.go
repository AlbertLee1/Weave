package oms

import "github.com/liyang/weave/pkg/apierror"

// ValidateObjectTypePrimaryKey enforces the US-004 rule that an ObjectType
// primary key must resolve to a real, non-derived property. Returns a wire
// ready apierror; nil on success.
func ValidateObjectTypePrimaryKey(primaryKey string, properties []Property) *apierror.APIError {
	if primaryKey == "" {
		return apierror.NewInvalidParameter("InvalidParameter:primaryKey", map[string]string{
			"parameter": "primaryKey",
			"reason":    "primaryKey is required",
		})
	}

	for _, p := range properties {
		if p.APIName != primaryKey {
			continue
		}
		if p.Derived {
			return apierror.NewInvalidParameter("DerivedPropertyNotAllowedAsPrimaryKey", map[string]string{
				"apiName": primaryKey,
				"reason":  "derived properties are computed at query time and cannot serve as a primary key",
			})
		}
		return nil
	}

	return apierror.NewInvalidParameter("InvalidParameter:primaryKey", map[string]string{
		"parameter": "primaryKey",
		"apiName":   primaryKey,
		"reason":    "primaryKey does not reference an existing property",
	})
}
