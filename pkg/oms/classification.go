package oms

// Data-classification vocabulary for ObjectType / Property metadata (US-262).
//
// The five constants are the fixed set that admins can choose from in the
// data-classification dropdown; the constants, not the strings, are the
// authoritative names so typos become compile errors.
const (
	ClassificationPublic       = "Public"
	ClassificationInternal     = "Internal"
	ClassificationConfidential = "Confidential"
	ClassificationPII          = "PII"
	ClassificationSecret       = "Secret"
)

// KnownClassifications returns the canonical ordered list of supported
// classification labels. Callers that need a set form (membership tests)
// should prefer IsKnownClassification.
func KnownClassifications() []string {
	return []string{
		ClassificationPublic,
		ClassificationInternal,
		ClassificationConfidential,
		ClassificationPII,
		ClassificationSecret,
	}
}

// IsKnownClassification reports whether s is a supported classification label.
// The empty string is treated as "unspecified" and also returns true so that
// callers can pass the raw field value without special-casing at every call
// site. When an admin request carries an explicit classification, handlers
// should reject empty + unknown separately (empty is a clear signal, unknown
// is a typo).
func IsKnownClassification(s string) bool {
	switch s {
	case "",
		ClassificationPublic,
		ClassificationInternal,
		ClassificationConfidential,
		ClassificationPII,
		ClassificationSecret:
		return true
	}
	return false
}
