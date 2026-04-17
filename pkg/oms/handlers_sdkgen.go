package oms

import (
	"archive/zip"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/sdkgen"
)

// GenerateSDK handles POST /api/v2/ontologies/{ontologyApiName}/sdkgen?lang=...
// Returns a zip file containing the generated SDK source files.
func (h *OMSHandler) GenerateSDK(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	lang := r.URL.Query().Get("lang")

	if lang == "" {
		apierror.WriteJSON(w, apierror.NewBadRequest("MissingLanguage", map[string]string{
			"detail": "query parameter 'lang' is required (ts|python|go)",
		}))
		return
	}

	gen, err := sdkgen.GetGenerator(lang)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewBadRequest("UnsupportedLanguage", map[string]string{
			"lang": lang,
		}))
		return
	}

	// Fetch full ontology export
	export, err := h.fetchOntologyExport(r, ontologyRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": ontologyRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("FetchOntologyFailed", nil))
		return
	}

	schema := buildOntologySchema(export)

	files, err := gen.Generate(r.Context(), schema)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("GenerateSDKFailed", nil))
		return
	}

	// Write zip to response
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"sdk.zip\"")
	w.WriteHeader(http.StatusOK)

	zw := zip.NewWriter(w)
	defer zw.Close()

	for _, f := range files {
		fw, err := zw.Create(f.Path)
		if err != nil {
			return
		}
		fw.Write(f.Content)
	}
}

// buildOntologySchema converts an OntologyExport to an sdkgen.OntologySchema.
func buildOntologySchema(export *OntologyExport) sdkgen.OntologySchema {
	schema := sdkgen.OntologySchema{
		Ontology: sdkgen.OntologyMeta{
			RID:         export.Ontology.RID,
			APIName:     export.Ontology.APIName,
			DisplayName: export.Ontology.DisplayName,
			Version:     export.Ontology.CurrentVersion,
		},
		ObjectTypes: make([]sdkgen.ObjectTypeSchema, 0, len(export.ObjectTypes)),
		LinkTypes:   make([]sdkgen.LinkTypeSchema, 0, len(export.LinkTypes)),
		ActionTypes: make([]sdkgen.ActionTypeSchema, 0, len(export.ActionTypes)),
		Interfaces:  make([]sdkgen.InterfaceSchema, 0, len(export.Interfaces)),
	}

	for _, ot := range export.ObjectTypes {
		props := make([]sdkgen.PropertySchema, 0, len(ot.Properties))
		for _, p := range ot.Properties {
			props = append(props, sdkgen.PropertySchema{
				APIName:  p.APIName,
				BaseType: p.BaseType,
				IsArray:  p.IsArray,
			})
		}
		schema.ObjectTypes = append(schema.ObjectTypes, sdkgen.ObjectTypeSchema{
			RID:         ot.RID,
			APIName:     ot.APIName,
			DisplayName: ot.DisplayName,
			PrimaryKey:  ot.PrimaryKey,
			Properties:  props,
		})
	}

	for _, lt := range export.LinkTypes {
		schema.LinkTypes = append(schema.LinkTypes, sdkgen.LinkTypeSchema{
			APIName:          lt.APIName,
			SourceObjectType: lt.SourceObjectType,
			TargetObjectType: lt.TargetObjectType,
			Cardinality:      lt.Cardinality,
		})
	}

	for _, at := range export.ActionTypes {
		schema.ActionTypes = append(schema.ActionTypes, sdkgen.ActionTypeSchema{
			APIName:     at.APIName,
			DisplayName: at.DisplayName,
			Parameters:  sdkgen.ParseActionParameters(at.Parameters),
		})
	}

	for _, iface := range export.Interfaces {
		schema.Interfaces = append(schema.Interfaces, sdkgen.InterfaceSchema{
			APIName:     iface.APIName,
			DisplayName: iface.DisplayName,
		})
	}

	return schema
}

// fetchOntologyExport loads a complete OntologyExport for the given ontology.
func (h *OMSHandler) fetchOntologyExport(r *http.Request, ontologyRID string) (*OntologyExport, error) {
	ctx := r.Context()

	ontology, err := h.repo.GetOntology(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}

	objectTypes, err := h.repo.ListObjectTypes(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}
	for i := range objectTypes {
		props, err := h.repo.ListProperties(ctx, objectTypes[i].RID)
		if err != nil {
			return nil, err
		}
		objectTypes[i].Properties = props
	}

	linkTypes, err := h.repo.ListLinkTypes(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}

	actionTypes, err := h.repo.ListActionTypes(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}

	interfaces, err := h.repo.ListInterfaces(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}

	sharedProperties, err := h.repo.ListSharedProperties(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}

	valueTypes, err := h.repo.ListValueTypes(ctx)
	if err != nil {
		return nil, err
	}

	typeGroups, err := h.repo.ListTypeGroups(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}

	functions, err := h.repo.ListFunctions(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}

	queryTypes, err := h.repo.ListQueryTypes(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}

	// Ensure non-nil slices
	if objectTypes == nil {
		objectTypes = []ObjectType{}
	}
	if linkTypes == nil {
		linkTypes = []LinkType{}
	}
	if actionTypes == nil {
		actionTypes = []ActionType{}
	}
	if interfaces == nil {
		interfaces = []Interface{}
	}
	if sharedProperties == nil {
		sharedProperties = []SharedProperty{}
	}
	if valueTypes == nil {
		valueTypes = []ValueType{}
	}
	if typeGroups == nil {
		typeGroups = []TypeGroup{}
	}
	if functions == nil {
		functions = []Function{}
	}
	if queryTypes == nil {
		queryTypes = []QueryType{}
	}

	return &OntologyExport{
		Ontology:         *ontology,
		ObjectTypes:      objectTypes,
		LinkTypes:        linkTypes,
		ActionTypes:      actionTypes,
		Interfaces:       interfaces,
		SharedProperties: sharedProperties,
		ValueTypes:       valueTypes,
		TypeGroups:       typeGroups,
		Functions:        functions,
		QueryTypes:       queryTypes,
	}, nil
}
