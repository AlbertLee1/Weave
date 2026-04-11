package index

// ScopedKey returns the per-ontology Bleve index key used to keep two
// ontologies' same-named ObjectTypes isolated. The Manager treats keys as
// opaque strings, so callers must compute the scoped key here before any
// EnsureIndex / IndexDocument / Search call. Format: "{ontologyApiName}__{objectType}".
func ScopedKey(ontologyAPIName, objectType string) string {
	return ontologyAPIName + "__" + objectType
}
