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
	r.Get("/api/v2/ontologies/{ontologyApiName}/fullMetadata", omsHandler.GetFullMetadata)

	// Admin routes
	r.Post("/api/admin/ontologies", omsHandler.CreateOntology)
	r.Put("/api/admin/ontologies/{ontologyRid}", omsHandler.UpdateOntology)

	r.Post("/api/admin/ontologies/{ontologyApiName}/objectTypes", omsHandler.CreateObjectType)
	r.Put("/api/admin/objectTypes/{objectTypeRid}", omsHandler.UpdateObjectType)
	r.Delete("/api/admin/objectTypes/{objectTypeRid}", omsHandler.DeleteObjectType)

	r.Post("/api/admin/objectTypes/{objectTypeRid}/properties", omsHandler.CreateProperty)
	r.Put("/api/admin/properties/{propertyRid}", omsHandler.UpdateProperty)
	r.Delete("/api/admin/properties/{propertyRid}", omsHandler.DeleteProperty)

	r.Post("/api/admin/ontologies/{ontologyApiName}/linkTypes", omsHandler.CreateLinkType)
	r.Get("/api/admin/ontologies/{ontologyApiName}/linkTypes", omsHandler.ListAllLinkTypes)
	r.Put("/api/admin/linkTypes/{linkTypeRid}", omsHandler.UpdateLinkType)
	r.Delete("/api/admin/linkTypes/{linkTypeRid}", omsHandler.DeleteLinkType)

	r.Post("/api/admin/ontologies/{ontologyApiName}/actionTypes", omsHandler.CreateActionType)
	r.Put("/api/admin/actionTypes/{actionTypeRid}", omsHandler.UpdateActionType)
	r.Delete("/api/admin/actionTypes/{actionTypeRid}", omsHandler.DeleteActionType)
	r.Get("/api/admin/actionTypes/{actionTypeRid}/logs", omsHandler.ListActionLogs)

	// Interface admin routes
	r.Post("/api/admin/ontologies/{ontologyApiName}/interfaces", omsHandler.CreateInterface)
	r.Get("/api/admin/ontologies/{ontologyApiName}/interfaces", omsHandler.ListInterfaces)
	r.Get("/api/admin/interfaces/{interfaceRid}", omsHandler.GetInterface)
	r.Put("/api/admin/interfaces/{interfaceRid}", omsHandler.UpdateInterface)
	r.Delete("/api/admin/interfaces/{interfaceRid}", omsHandler.DeleteInterface)
	r.Post("/api/admin/objectTypes/{objectTypeRid}/interfaces", omsHandler.AttachInterfaceHandler)
	r.Delete("/api/admin/objectTypes/{objectTypeRid}/interfaces/{interfaceRid}", omsHandler.DetachInterface)
	r.Get("/api/admin/objectTypes/{objectTypeRid}/interfaces", omsHandler.ListObjectTypeInterfaces)

	// Shared Property admin routes
	r.Post("/api/admin/ontologies/{ontologyApiName}/shared-properties", omsHandler.CreateSharedProperty)
	r.Get("/api/admin/ontologies/{ontologyApiName}/shared-properties", omsHandler.ListSharedProperties)
	r.Get("/api/admin/shared-properties/{sharedPropertyRid}", omsHandler.GetSharedProperty)
	r.Put("/api/admin/shared-properties/{sharedPropertyRid}", omsHandler.UpdateSharedProperty)
	r.Delete("/api/admin/shared-properties/{sharedPropertyRid}", omsHandler.DeleteSharedProperty)

	// Type Group admin routes
	r.Post("/api/admin/ontologies/{ontologyApiName}/type-groups", omsHandler.CreateTypeGroup)
	r.Get("/api/admin/ontologies/{ontologyApiName}/type-groups", omsHandler.ListTypeGroups)
	r.Get("/api/admin/type-groups/{typeGroupRid}", omsHandler.GetTypeGroup)
	r.Put("/api/admin/type-groups/{typeGroupRid}", omsHandler.UpdateTypeGroup)
	r.Delete("/api/admin/type-groups/{typeGroupRid}", omsHandler.DeleteTypeGroup)
	r.Post("/api/admin/objectTypes/{objectTypeRid}/groups/{typeGroupRid}", omsHandler.AssignTypeGroup)
	r.Delete("/api/admin/objectTypes/{objectTypeRid}/groups/{typeGroupRid}", omsHandler.RemoveTypeGroup)
	r.Get("/api/admin/objectTypes/{objectTypeRid}/groups", omsHandler.ListTypeGroupsForObjectType)

	// Value Type admin routes
	r.Post("/api/admin/value-types", omsHandler.CreateValueType)
	r.Get("/api/admin/value-types", omsHandler.ListValueTypes)
	r.Get("/api/admin/value-types/{valueTypeRid}", omsHandler.GetValueType)
	r.Put("/api/admin/value-types/{valueTypeRid}", omsHandler.UpdateValueType)
	r.Delete("/api/admin/value-types/{valueTypeRid}", omsHandler.DeleteValueType)

	// DatasourceBinding admin routes
	r.Post("/api/admin/objectTypes/{objectTypeRid}/datasourceBindings", omsHandler.CreateDatasourceBinding)
	r.Get("/api/admin/objectTypes/{objectTypeRid}/datasourceBindings", omsHandler.ListDatasourceBindings)
	r.Get("/api/admin/datasourceBindings/{datasourceBindingRid}", omsHandler.GetDatasourceBinding)
	r.Put("/api/admin/datasourceBindings/{datasourceBindingRid}", omsHandler.UpdateDatasourceBinding)
	r.Delete("/api/admin/datasourceBindings/{datasourceBindingRid}", omsHandler.DeleteDatasourceBinding)

	// SecurityPolicy admin routes
	r.Post("/api/admin/objectTypes/{objectTypeRid}/securityPolicies", omsHandler.CreateSecurityPolicy)
	r.Get("/api/admin/objectTypes/{objectTypeRid}/securityPolicies", omsHandler.ListSecurityPolicies)
	r.Get("/api/admin/securityPolicies/{securityPolicyRid}", omsHandler.GetSecurityPolicy)
	r.Put("/api/admin/securityPolicies/{securityPolicyRid}", omsHandler.UpdateSecurityPolicy)
	r.Delete("/api/admin/securityPolicies/{securityPolicyRid}", omsHandler.DeleteSecurityPolicy)

	// QueryType admin routes
	r.Post("/api/admin/ontologies/{ontologyApiName}/queryTypes", omsHandler.CreateQueryType)
	r.Get("/api/admin/ontologies/{ontologyApiName}/queryTypes", omsHandler.ListQueryTypes)
	r.Get("/api/admin/queryTypes/{queryTypeRid}", omsHandler.GetQueryType)
	r.Put("/api/admin/queryTypes/{queryTypeRid}", omsHandler.UpdateQueryType)
	r.Delete("/api/admin/queryTypes/{queryTypeRid}", omsHandler.DeleteQueryType)

	// QueryType execute route
	r.Post("/api/v2/ontologies/{ontology}/queries/{queryApiName}/execute", omsHandler.ExecuteQueryType)

	// Search, Export, Import
	r.Get("/api/admin/ontologies/{ontologyApiName}/search", omsHandler.SearchOntologyResources)
	r.Get("/api/admin/ontologies/{ontologyApiName}/export", omsHandler.ExportOntology)
	r.Post("/api/admin/ontologies/import", omsHandler.ImportOntology)

	// Snapshot routes
	r.Post("/api/admin/ontologies/{ontologyApiName}/snapshots", omsHandler.CreateSnapshot)
	r.Get("/api/admin/ontologies/{ontologyApiName}/snapshots", omsHandler.ListSnapshots)
	r.Get("/api/admin/ontologies/{ontologyApiName}/snapshots/{version}", omsHandler.GetSnapshot)
}
