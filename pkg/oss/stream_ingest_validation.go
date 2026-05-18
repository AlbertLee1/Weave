package oss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/types"
)

// IngestMetadataValidator validates stream ingest edits against ontology
// metadata before the handler publishes a batch to the funnel.
type IngestMetadataValidator interface {
	ValidateIngestEdits(ctx context.Context, ontologyAPIName, objectType string, edits []funnel.Edit) *apierror.APIError
}

// IngestMetadataRepository is the narrow OMS surface needed by the ingest
// validator. oms.Repository satisfies it, and tests can provide small fakes.
type IngestMetadataRepository interface {
	GetObjectTypeByAPIName(ctx context.Context, ontologyAPIName, apiName string) (*oms.ObjectType, error)
	ListProperties(ctx context.Context, objectTypeRID string) ([]oms.Property, error)
	GetValueTypeByAPIName(ctx context.Context, ridOrAPIName string) (*oms.ValueType, error)
}

type streamIngestMetadataValidator struct {
	repo IngestMetadataRepository
}

// NewStreamIngestMetadataValidator creates a validator backed by OMS metadata.
func NewStreamIngestMetadataValidator(repo IngestMetadataRepository) IngestMetadataValidator {
	return &streamIngestMetadataValidator{repo: repo}
}

func (v *streamIngestMetadataValidator) ValidateIngestEdits(ctx context.Context, ontologyAPIName, objectType string, edits []funnel.Edit) *apierror.APIError {
	if v == nil || v.repo == nil || len(edits) == 0 {
		return nil
	}

	ot, err := v.repo.GetObjectTypeByAPIName(ctx, ontologyAPIName, objectType)
	if err != nil || ot == nil {
		if errors.Is(err, oms.ErrNotFound) || ot == nil {
			return apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"ontology":   ontologyAPIName,
				"objectType": objectType,
			})
		}
		return apierror.NewInternal("IngestMetadataLookupFailed", map[string]string{
			"ontology":   ontologyAPIName,
			"objectType": objectType,
			"error":      err.Error(),
		})
	}

	properties, err := v.repo.ListProperties(ctx, ot.RID)
	if err != nil {
		return apierror.NewInternal("IngestMetadataLookupFailed", map[string]string{
			"ontology":   ontologyAPIName,
			"objectType": objectType,
			"error":      err.Error(),
		})
	}

	declared := make(map[string]struct{}, len(properties))
	valueTypeByProperty := make(map[string]string)
	for _, p := range properties {
		declared[p.APIName] = struct{}{}
		vtAPIName := valueTypeAPIName(p.TypeConfig)
		if vtAPIName != "" {
			valueTypeByProperty[p.APIName] = vtAPIName
		}
	}

	if apiErr := validateIngestEditSchema(edits, declared); apiErr != nil {
		return apiErr
	}
	return v.validateValueTypes(ctx, ontologyAPIName, objectType, edits, valueTypeByProperty)
}

func validateIngestEditSchema(edits []funnel.Edit, declared map[string]struct{}) *apierror.APIError {
	var first *funnel.Edit
	firstProperty := ""
	violationCount := 0

	for i := range edits {
		edit := &edits[i]
		if !ingestEditHasObjectProperties(*edit) {
			continue
		}
		for _, property := range sortedPropertyNames(edit.Properties) {
			if _, ok := declared[property]; ok {
				continue
			}
			violationCount++
			if first == nil {
				first = edit
				firstProperty = property
			}
		}
	}

	if violationCount == 0 {
		return nil
	}
	return apierror.NewBadRequest("SchemaViolation", map[string]string{
		"objectType":     first.ObjectType,
		"primaryKey":     first.PrimaryKey,
		"property":       firstProperty,
		"violationCount": fmt.Sprintf("%d", violationCount),
	})
}

func (v *streamIngestMetadataValidator) validateValueTypes(
	ctx context.Context,
	ontologyAPIName string,
	objectType string,
	edits []funnel.Edit,
	valueTypeByProperty map[string]string,
) *apierror.APIError {
	for _, edit := range edits {
		if !ingestEditHasObjectProperties(edit) {
			continue
		}
		for _, property := range sortedPropertyNames(edit.Properties) {
			vtAPIName, ok := valueTypeByProperty[property]
			if !ok {
				continue
			}
			vt, err := v.repo.GetValueTypeByAPIName(ctx, vtAPIName)
			if err != nil || vt == nil {
				continue
			}
			value := edit.Properties[property]
			if err := types.ValidateConstraints(value, vt.Constraints); err != nil {
				var enumErr *types.EnumViolationError
				if errors.As(err, &enumErr) {
					return apierror.NewValidationEnum("EnumViolation", map[string]string{
						"ontology":      ontologyAPIName,
						"objectType":    objectType,
						"primaryKey":    edit.PrimaryKey,
						"property":      property,
						"value":         fmt.Sprint(value),
						"allowedValues": strings.Join(enumErr.AllowedValues, ","),
					})
				}
				return apierror.NewInvalidParameter("ValueTypeConstraintViolation", map[string]string{
					"ontology":   ontologyAPIName,
					"objectType": objectType,
					"primaryKey": edit.PrimaryKey,
					"property":   property,
					"value":      fmt.Sprint(value),
					"reason":     err.Error(),
				})
			}
		}
	}
	return nil
}

func ingestEditHasObjectProperties(edit funnel.Edit) bool {
	if edit.Type == funnel.EditTypeDelete ||
		edit.Type == funnel.EditTypeLinkCreate ||
		edit.Type == funnel.EditTypeLinkDelete {
		return false
	}
	return len(edit.Properties) > 0
}

func sortedPropertyNames(properties map[string]interface{}) []string {
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func valueTypeAPIName(typeConfig json.RawMessage) string {
	if len(typeConfig) == 0 {
		return ""
	}
	var decoded struct {
		ValueTypeAPIName string `json:"valueTypeApiName"`
	}
	if err := json.Unmarshal(typeConfig, &decoded); err != nil {
		return ""
	}
	return decoded.ValueTypeAPIName
}
