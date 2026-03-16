package main

import (
	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// RegisterRoutes registers all OMS routes on the given router.
func RegisterRoutes(r chi.Router, omsHandler *oms.OMSHandler) {
	// V2 read-only routes
	r.Get("/api/v2/ontologies", omsHandler.ListOntologies)
	r.Get("/api/v2/ontologies/{ontologyApiName}", omsHandler.GetOntology)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes", omsHandler.ListObjectTypes)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}", omsHandler.GetObjectType)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/outgoingLinkTypes", omsHandler.ListOutgoingLinkTypes)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes", omsHandler.ListActionTypes)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}", omsHandler.GetActionType)

	// Admin routes
	r.Post("/api/admin/ontologies", omsHandler.CreateOntology)
	r.Post("/api/admin/ontologies/{ontologyApiName}/objectTypes", omsHandler.CreateObjectType)
	r.Put("/api/admin/objectTypes/{objectTypeRid}", omsHandler.UpdateObjectType)
	r.Delete("/api/admin/objectTypes/{objectTypeRid}", omsHandler.DeleteObjectType)
	r.Post("/api/admin/objectTypes/{objectTypeRid}/properties", omsHandler.CreateProperty)
	r.Delete("/api/admin/properties/{propertyRid}", omsHandler.DeleteProperty)
	r.Post("/api/admin/ontologies/{ontologyApiName}/linkTypes", omsHandler.CreateLinkType)
	r.Post("/api/admin/ontologies/{ontologyApiName}/actionTypes", omsHandler.CreateActionType)
	r.Put("/api/admin/actionTypes/{actionTypeRid}", omsHandler.UpdateActionType)
}
