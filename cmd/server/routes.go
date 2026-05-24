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
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes", omsHandler.CreateObjectType)
	r.Put("/api/v2/ontologies/{ontologyApiName}/objectTypes/byRid/{objectTypeRid}", omsHandler.UpdateObjectType)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/objectTypes/byRid/{objectTypeRid}", omsHandler.DeleteObjectType)
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/getByRidBatch", omsHandler.GetObjectTypesByRidBatchV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}", omsHandler.GetObjectType)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/fullMetadata", omsHandler.GetObjectTypeFullMetadataV2)
	// US-212 Object Type Inheritance: returns the type with parent properties
	// + outgoing links merged (child overrides on api_name match).
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/resolved", omsHandler.GetObjectTypeResolved)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/outgoingLinkTypes", omsHandler.ListOutgoingLinkTypes)
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectTypeApiName}/editsHistory", omsHandler.PostObjectTypeEditsHistoryV2)
	// Property admin CRUD (US-147)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/byRid/{objectTypeRid}/properties", omsHandler.ListPropertiesForObjectTypeAdmin)
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/byRid/{objectTypeRid}/properties", omsHandler.CreateProperty)
	r.Put("/api/v2/ontologies/{ontologyApiName}/properties/byRid/{propertyRid}", omsHandler.UpdateProperty)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/properties/byRid/{propertyRid}", omsHandler.DeleteProperty)
	// LinkType admin CRUD (US-148)
	r.Get("/api/v2/ontologies/{ontologyApiName}/linkTypes", omsHandler.ListLinkTypesForOntologyAdmin)
	r.Post("/api/v2/ontologies/{ontologyApiName}/linkTypes", omsHandler.CreateLinkType)
	r.Put("/api/v2/ontologies/{ontologyApiName}/linkTypes/byRid/{linkTypeRid}", omsHandler.UpdateLinkType)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/linkTypes/byRid/{linkTypeRid}", omsHandler.DeleteLinkType)
	// Foundry-1:1 read by API name (mirrors GET /objectTypes/{objectTypeApiName}).
	// SDKs hit this after a search response surfaces a linkType slug they
	// need to render — without it, callers fall back to ListLinkTypes +
	// client-side filter on every call. MUST be declared AFTER the
	// /linkTypes/byRid/{linkTypeRid} routes above so chi's path matcher
	// gives the more-specific byRid prefix priority and `byRid` is never
	// captured as a literal {linkType} slug.
	r.Get("/api/v2/ontologies/{ontologyApiName}/linkTypes/{linkType}", omsHandler.GetLinkTypeByAPIName)
	// LinkProperty admin CRUD (US-210): schema lives per LinkType; edge values
	// ride on link_edges.edge_properties JSONB and are written via the edges
	// PUT endpoint below.
	r.Get("/api/v2/ontologies/{ontologyApiName}/links/{linkTypeRid}/properties", omsHandler.ListLinkProperties)
	r.Post("/api/v2/ontologies/{ontologyApiName}/links/{linkTypeRid}/properties", omsHandler.CreateLinkProperty)
	r.Put("/api/v2/ontologies/{ontologyApiName}/links/properties/byRid/{linkPropertyRid}", omsHandler.UpdateLinkProperty)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/links/properties/byRid/{linkPropertyRid}", omsHandler.DeleteLinkProperty)
	// US-210 edge-value upsert: replaces link_edges.edge_properties for the
	// (linkTypeRid, sourcePk, targetPk) edge, validating values against the
	// LinkType's declared link-property schema.
	r.Put("/api/v2/ontologies/{ontologyApiName}/links/{linkTypeRid}/edges/{sourcePk}/{targetPk}/properties", omsHandler.PutLinkEdgeProperties)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes", omsHandler.ListActionTypes)
	r.Post("/api/v2/ontologies/{ontologyApiName}/actionTypes", omsHandler.CreateActionType)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/byRid/{actionTypeRid}", omsHandler.GetActionTypeByRidV2)
	r.Put("/api/v2/ontologies/{ontologyApiName}/actionTypes/byRid/{actionTypeRid}", omsHandler.UpdateActionType)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/actionTypes/byRid/{actionTypeRid}", omsHandler.DeleteActionType)
	r.Post("/api/v2/ontologies/{ontologyApiName}/actionTypes/getByRidBatch", omsHandler.GetActionTypesByRidBatchV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}", omsHandler.GetActionType)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypes/{actionTypeRid}/fullMetadata", omsHandler.GetActionTypeFullMetadataV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypesFullMetadata", omsHandler.ListActionTypesFullMetadataV2)
	// ActionType admin CRUD (US-149): list endpoint exposes rules for the visual builder
	r.Get("/api/v2/ontologies/{ontologyApiName}/actionTypesAdmin", omsHandler.ListActionTypesForOntologyAdmin)
	// Interface admin CRUD (US-150)
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfacesAdmin", omsHandler.ListInterfacesForOntologyAdmin)
	r.Post("/api/v2/ontologies/{ontologyApiName}/interfaces", omsHandler.CreateInterface)
	r.Put("/api/v2/ontologies/{ontologyApiName}/interfaces/byRid/{interfaceRid}", omsHandler.UpdateInterface)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/interfaces/byRid/{interfaceRid}", omsHandler.DeleteInterface)
	// US-214 Interface Method Signatures: CRUD on methods declared on an
	// Interface + the polymorphic invoke endpoint that dispatches to the
	// ActionType implementing that method for a given ObjectType.
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaces/{interfaceRid}/methods", omsHandler.ListInterfaceMethods)
	r.Post("/api/v2/ontologies/{ontologyApiName}/interfaces/{interfaceRid}/methods", omsHandler.CreateInterfaceMethod)
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaces/methods/byRid/{methodRid}", omsHandler.GetInterfaceMethod)
	r.Put("/api/v2/ontologies/{ontologyApiName}/interfaces/methods/byRid/{methodRid}", omsHandler.UpdateInterfaceMethod)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/interfaces/methods/byRid/{methodRid}", omsHandler.DeleteInterfaceMethod)
	r.Post("/api/v2/ontologies/{ontologyApiName}/interfaces/methods/{methodRid}/invoke", omsHandler.InvokeInterfaceMethod)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/byRid/{objectTypeRid}/interfaces", omsHandler.ListObjectTypeInterfaces)
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/byRid/{objectTypeRid}/interfaces", omsHandler.AttachInterfaceHandler)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/objectTypes/byRid/{objectTypeRid}/interfaces/{interfaceRid}", omsHandler.DetachInterface)
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaceTypes", omsHandler.ListInterfaceTypesV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaceTypes/{interfaceType}/outgoingLinkTypes/{interfaceLinkType}", omsHandler.GetInterfaceOutgoingLinkTypeV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaceTypes/{interfaceType}/outgoingLinkTypes", omsHandler.ListInterfaceOutgoingLinkTypesV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaceTypes/{interfaceType}", omsHandler.GetInterfaceTypeV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/valueTypes", omsHandler.ListValueTypesV2)
	// ValueType admin CRUD + Used-By reverse lookup (US-051 / PC-A05). The
	// `valueTypesAdmin` list endpoint returns the same envelope without the
	// preview-gate the runtime V2 list above enforces, mirroring the
	// `interfacesAdmin` / `actionTypesAdmin` admin-list pattern. Mutations
	// reuse the existing CreateValueType / UpdateValueType / DeleteValueType
	// handlers — they were authored for the deleted `/api/admin/value-types`
	// routes (US-006) and remain correct in shape, just newly re-mounted.
	r.Get("/api/v2/ontologies/{ontologyApiName}/valueTypesAdmin", omsHandler.ListValueTypes)
	r.Post("/api/v2/ontologies/{ontologyApiName}/valueTypes", omsHandler.CreateValueType)
	r.Put("/api/v2/ontologies/{ontologyApiName}/valueTypes/byRid/{valueTypeRid}", omsHandler.UpdateValueType)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/valueTypes/byRid/{valueTypeRid}", omsHandler.DeleteValueType)
	r.Get("/api/v2/ontologies/{ontologyApiName}/valueTypes/byRid/{valueTypeRid}/usages", omsHandler.ListValueTypeUsages)
	r.Get("/api/v2/ontologies/{ontologyApiName}/valueTypes/{valueType}", omsHandler.GetValueTypeV2)
	// DatasourceBinding admin CRUD (US-052 / PC-A06). The handlers were
	// authored for the deleted /api/admin/objectTypes/{rid}/datasourceBindings
	// and /api/admin/datasourceBindings/{rid} routes (US-006); this re-mounts
	// them under the Foundry-aligned V2 paths so the ObjectType Bindings tab
	// (web/src/components/admin/BindingsEditor) can drive Create / List /
	// Get / Update / Delete. The handlers also synchronously derive
	// column-level lineage edges (US-377) — that side-effect is preserved.
	r.Get("/api/v2/ontologies/{ontologyApiName}/objectTypes/byRid/{objectTypeRid}/datasourceBindings", omsHandler.ListDatasourceBindings)
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/byRid/{objectTypeRid}/datasourceBindings", omsHandler.CreateDatasourceBinding)
	r.Get("/api/v2/ontologies/{ontologyApiName}/datasourceBindings/byRid/{datasourceBindingRid}", omsHandler.GetDatasourceBinding)
	r.Put("/api/v2/ontologies/{ontologyApiName}/datasourceBindings/byRid/{datasourceBindingRid}", omsHandler.UpdateDatasourceBinding)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/datasourceBindings/byRid/{datasourceBindingRid}", omsHandler.DeleteDatasourceBinding)
	r.Get("/api/v2/ontologies/{ontologyApiName}/queryTypes", omsHandler.ListQueryTypesV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/queryTypes/{queryApiName}", omsHandler.GetQueryTypeV2)
	r.Get("/api/v2/ontologies/{ontologyApiName}/fullMetadata", omsHandler.GetFullMetadata)
	r.Get("/api/v2/ontologies/{ontologyApiName}/export", omsHandler.ExportOntologyV2)
	r.Post("/api/v2/ontologies/import", omsHandler.ImportOntologyV2)
	r.Post("/api/v2/ontologies/{ontologyApiName}/metadata", omsHandler.LoadMetadataV2)

	// Package install + marketplace registry (US-412). The install endpoint
	// is mounted unconditionally; it falls back to no-op recording when no
	// InstalledPackageStore is wired so degraded-mode boots still serve the
	// import path. The list/toggle/delete endpoints surface 404
	// InstalledPackagesNotConfigured in the same degraded mode so the
	// marketplace UI (US-413, US-454) renders an appropriate empty state.
	r.Post("/api/v2/pkg/install", omsHandler.PackageInstall)
	r.Get("/api/v2/pkg", omsHandler.ListInstalledPackages)
	// Built-in catalog (US-414). Mounted unconditionally; the list endpoint
	// returns an empty data array when no provider is wired, and the install
	// endpoint surfaces 404 BuiltinPackagesNotConfigured. Both routes are
	// declared BEFORE the {name} catch-all below so `/api/v2/pkg/builtin`
	// doesn't collide with the parameterised `/api/v2/pkg/{name}` getter.
	r.Get("/api/v2/pkg/builtin", omsHandler.ListBuiltinPackages)
	r.Post("/api/v2/pkg/builtin/{slug}/install", omsHandler.InstallBuiltinPackage)
	r.Get("/api/v2/pkg/{name}", omsHandler.GetInstalledPackage)
	r.Post("/api/v2/pkg/{name}/enabled", omsHandler.SetInstalledPackageEnabled)
	r.Delete("/api/v2/pkg/{name}", omsHandler.DeleteInstalledPackage)

	// SDK Generation (US-136)
	r.Post("/api/v2/ontologies/{ontologyApiName}/sdkgen", omsHandler.GenerateSDK)

	// Function CRUD (US-089). The {functionRid} segment accepts a RID, a
	// bare name, or `name@version` (US-217 semver pinning); resolution lives
	// in OMSHandler.resolveFunctionRef.
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions", omsHandler.CreateFunction)
	r.Get("/api/v2/ontologies/{ontologyApiName}/functions", omsHandler.ListFunctions)
	r.Get("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}", omsHandler.GetFunctionV2)
	r.Put("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}", omsHandler.UpdateFunction)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}", omsHandler.DeleteFunction)
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/execute", omsHandler.ExecuteFunction)
	// US-217 list-versions: returns every stored semver of the named function
	// in the ontology, latest-first.
	r.Get("/api/v2/ontologies/{ontologyApiName}/functions/{functionName}/versions", omsHandler.ListFunctionVersions)
	// US-370 deterministic replay: re-runs a Function at the captured version
	// against the recorded input and surfaces a hash divergence as
	// WEAVE_FUNCTION_NONDETERMINISTIC.
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/replay", omsHandler.ReplayFunction)
	// US-475 top-level alias: same replay contract but without an ontology
	// URL parameter, so SDK clients that hold only a Function RID can hit
	// /replay without first mapping back to the ontology api name. The
	// functionRid MUST be RID-shaped (`ri.*`); bare-name refs are 400'd at
	// the handler because there is no ontology context to disambiguate.
	r.Post("/api/v2/functions/{functionRid}/replay", omsHandler.ReplayFunctionByRID)

	// US-415 Function code repository: per-function bare git repo at
	// `data/repos/{rid}/.git`. POST /commits records a new revision of the
	// source code; GET /log returns the newest-first commit list. Both
	// routes report 503 FunctionRepoNotConfigured when no FunctionRepoStore
	// is wired so degraded-mode test routers still boot cleanly.
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/commits", omsHandler.CreateFunctionRepoCommit)
	r.Get("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/log", omsHandler.ListFunctionRepoCommits)
	// US-416 Function PR / Diff UI: fetch one historical revision (commit
	// metadata + source code blob) so the SPA can render a side-by-side
	// diff between any two commits without re-walking the log.
	r.Get("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/commits/{hash}", omsHandler.GetFunctionRepoCommit)
	// US-417 Function CI Hook: per-commit lint+test job state. The POST
	// /commits handler queues the job in the background; this endpoint
	// returns the latest result so the SPA can render a ✅/❌ badge next
	// to the commit hash. 503 CommitJobsNotConfigured when no store is
	// wired; 404 CommitJobNotFound when the commit was not picked up by
	// the hook.
	r.Get("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/commits/{hash}/job", omsHandler.GetFunctionRepoCommitJob)

	// Ontology Branches (US-113, US-116)
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches", omsHandler.CreateBranch)
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches", omsHandler.ListBranches)
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}", omsHandler.GetBranch)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}", omsHandler.CloseBranch)
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/diff", omsHandler.GetBranchDiff)
	// US-385 — POST diff returns the categorised added/modified/deleted view
	// plus inline conflict annotations (apiName + resolutionKey) the merge
	// handler will look up against conflictResolution.
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/diff", omsHandler.PostBranchDiff)
	r.Get("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/breaking-changes", omsHandler.GetBranchBreakingChanges)
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/rebase", omsHandler.RebaseBranch)
	// US-385 — direct branch merge with conflictResolution body. Sits next
	// to the proposal-based MergeProposal flow; both write to the same
	// underlying branch_changes apply path.
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/merge", omsHandler.MergeBranch)

	// Ontology Proposals (US-117, US-118)
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals", omsHandler.CreateProposal)
	r.Get("/api/v2/ontologies/{ontologyApiName}/proposals", omsHandler.ListProposals)
	r.Get("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}", omsHandler.GetProposal)
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/approve", omsHandler.ApproveProposal)
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/reject", omsHandler.RejectProposal)
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/merge", omsHandler.MergeProposal)

	// Automation Rules (US-125)
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules", omsHandler.CreateAutomationRule)
	r.Get("/api/v2/ontologies/{ontologyApiName}/automationRules", omsHandler.ListAutomationRules)
	r.Get("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}", omsHandler.GetAutomationRule)
	r.Put("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}", omsHandler.UpdateAutomationRule)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}", omsHandler.DeleteAutomationRule)
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/pause", omsHandler.PauseAutomationRule)
	r.Post("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/resume", omsHandler.ResumeAutomationRule)
	r.Get("/api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/executions", omsHandler.ListExecutions)

	// Notifications (US-130, US-343)
	r.Get("/api/v2/notifications", omsHandler.ListNotifications)
	r.Post("/api/v2/notifications/read-all", omsHandler.MarkAllNotificationsRead)
	r.Post("/api/v2/notifications/{notificationId}/read", omsHandler.MarkNotificationRead)

	// QueryType execute route
	r.Post("/api/v2/ontologies/{ontologyApiName}/queries/{queryApiName}/execute", omsHandler.ExecuteQueryType)
}
