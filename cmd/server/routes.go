package main

import (
	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// RegisterRoutes registers all OMS routes on the given router. Only
// Foundry-aligned V2 read-only endpoints and query execution remain;
// all /api/admin/* routes were removed in US-006.
func RegisterRoutes(r chi.Router, omsHandler *oms.OMSHandler) {
	// V2 read-only routes
	r.Get("/api/v2/ontologies", omsHandler.ListOntologies)
	r.Get("/api/v2/ontologies/{ontologyApiName}", omsHandler.GetOntology)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes", omsHandler.ListObjectTypes)
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/getByRidBatch", omsHandler.GetObjectTypesByRidBatchV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}", omsHandler.GetObjectType)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/fullMetadata", omsHandler.GetObjectTypeFullMetadataV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/outgoingLinkTypes", omsHandler.ListOutgoingLinkTypes)
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/editsHistory", omsHandler.PostObjectTypeEditsHistoryV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes", omsHandler.ListActionTypes)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/byRid/{actionTypeRid}", omsHandler.GetActionTypeByRidV2)
	r.Post("/api/v2/ontologies/{ontologyApiName}/actionTypes/getByRidBatch", omsHandler.GetActionTypesByRidBatchV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}", omsHandler.GetActionType)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}/fullMetadata", omsHandler.GetActionTypeFullMetadataV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypesFullMetadata", omsHandler.ListActionTypesFullMetadataV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaceTypes", omsHandler.ListInterfaceTypesV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaceTypes/{interfaceType}/outgoingLinkTypes/{interfaceLinkType}", omsHandler.GetInterfaceOutgoingLinkTypeV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaceTypes/{interfaceType}/outgoingLinkTypes", omsHandler.ListInterfaceOutgoingLinkTypesV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaceTypes/{interfaceType}", omsHandler.GetInterfaceTypeV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/valueTypes", omsHandler.ListValueTypesV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/valueTypes/{valueType}", omsHandler.GetValueTypeV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/queryTypes", omsHandler.ListQueryTypesV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/queryTypes/{queryApiName}", omsHandler.GetQueryTypeV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/fullMetadata", omsHandler.GetFullMetadata)
	r.Get("/api/v2/ontologies/{ontologyApiName}/export", omsHandler.ExportOntologyV2)
	r.Post("/api/v2/ontologies/{ontologyApiName}/metadata", omsHandler.LoadMetadataV2)

	// Function CRUD (US-089)
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions", omsHandler.CreateFunction)
	r.Get("/api/v2/ontologies/{ontologyApiName}/functions", omsHandler.ListFunctions)
	r.Get("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}", omsHandler.GetFunctionV2)
	r.Put("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}", omsHandler.UpdateFunction)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}", omsHandler.DeleteFunction)

	// Ontology Branches (US-113, US-116)
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches", omsHandler.CreateBranch)
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches", omsHandler.ListBranches)
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}", omsHandler.GetBranch)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}", omsHandler.CloseBranch)
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/diff", omsHandler.GetBranchDiff)
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/rebase", omsHandler.RebaseBranch)

	// Ontology Proposals (US-117, US-118)
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals", omsHandler.CreateProposal)
	r.Get("/api/v2/ontologies/{ontologyApiName}/proposals", omsHandler.ListProposals)
	r.Get("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}", omsHandler.GetProposal)
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/approve", omsHandler.ApproveProposal)
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/reject", omsHandler.RejectProposal)
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/merge", omsHandler.MergeProposal)

	// QueryType execute route
	r.Post("/api/v2/ontologies/{ontologyApiName}/queries/{queryApiName}/execute", omsHandler.ExecuteQueryType)
}
