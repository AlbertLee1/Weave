package oms

import (
	"context"
	"errors"

	"github.com/liyang/weave/pkg/apierror"
)

// validateCompensateActionRID verifies the compensating ActionType pointer
// (US-239). The partner must exist, live in the same ontology as the owning
// ActionType, and must not be a self-reference — an action cannot be its own
// saga compensator.
func (h *OMSHandler) validateCompensateActionRID(ctx context.Context, ontologyRID, selfRID, compensateRID string) *apierror.APIError {
	if compensateRID == selfRID {
		return apierror.NewInvalidParameter("InvalidParameter:compensateActionRid", map[string]string{
			"parameter": "compensateActionRid",
			"reason":    "action cannot compensate itself",
		})
	}
	partner, err := h.repo.GetActionType(ctx, compensateRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return apierror.NewNotFound("CompensateActionTypeNotFound", map[string]string{
				"compensateActionRid": compensateRID,
			})
		}
		return apierror.NewInternal("GetActionTypeFailed", nil)
	}
	if partner.OntologyRID != ontologyRID {
		return apierror.NewInvalidParameter("InvalidParameter:compensateActionRid", map[string]string{
			"parameter": "compensateActionRid",
			"reason":    "compensator must live in the same ontology",
		})
	}
	return nil
}
