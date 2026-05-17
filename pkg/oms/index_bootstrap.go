package oms

// IndexBootstrapper is the narrow capability the OMS handlers consult so a
// newly created or freshly re-imported ObjectType has an open Bleve index
// shell before the request returns. DOG-003 traced the dogfood "index not
// found for object type" failure to a race between import succeeding and the
// funnel consumer's first IndexDocument call: the consumer had no metadata
// hook to create the index lazily, so the very first ingest batch silently
// dropped its docs.
//
// The interface is intentionally narrow — implementations only need to know
// (ontologyAPIName, objectTypeAPIName) and the property schema. cmd/server
// wires a *index.Manager adapter that computes the scoped key, translates
// oms.Property → index.Property, and calls EnsureIndex / DropIndex. A nil
// bootstrapper makes every method a no-op, so degraded-mode test routers
// without a Bleve directory still pass admin-CRUD tests.
type IndexBootstrapper interface {
	// EnsureObjectTypeIndex opens or creates the Bleve index for the given
	// ObjectType using the supplied property schema. Idempotent — repeated
	// calls with the same arguments are safe.
	EnsureObjectTypeIndex(ontologyAPIName, objectTypeAPIName string, props []Property) error
	// DropObjectTypeIndex closes and removes the Bleve index for the given
	// ObjectType. Used by replace-mode import so stale rows from a prior
	// schema cannot leak into the new index. Idempotent.
	DropObjectTypeIndex(ontologyAPIName, objectTypeAPIName string) error
}

// SetIndexBootstrapper wires the optional IndexBootstrapper used by the
// create-object-type and import paths to materialise the Bleve index shell
// synchronously before the response is written. Pass nil to disable
// (the historical pre-DOG-003 behaviour where indexes were only created
// on demand by the funnel consumer, which silently failed for brand-new
// ObjectTypes).
func (h *OMSHandler) SetIndexBootstrapper(b IndexBootstrapper) {
	h.indexBootstrapper = b
}

// ensureObjectTypeIndex is the internal hook the create / import paths call
// after a successful CreateObjectType. Errors are logged by the caller but
// must not abort the request — index materialisation can be re-run via the
// admin rebuild endpoint, but the ObjectType row has already been persisted.
func (h *OMSHandler) ensureObjectTypeIndex(ontologyAPIName, objectTypeAPIName string, props []Property) error {
	if h.indexBootstrapper == nil {
		return nil
	}
	return h.indexBootstrapper.EnsureObjectTypeIndex(ontologyAPIName, objectTypeAPIName, props)
}

// dropObjectTypeIndex mirrors ensureObjectTypeIndex for replace-mode import,
// guaranteeing stale Bleve docs from the previous ObjectType are removed
// before the new index is recreated and the next ingest is accepted.
func (h *OMSHandler) dropObjectTypeIndex(ontologyAPIName, objectTypeAPIName string) error {
	if h.indexBootstrapper == nil {
		return nil
	}
	return h.indexBootstrapper.DropObjectTypeIndex(ontologyAPIName, objectTypeAPIName)
}
