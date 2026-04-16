package oms

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/rid"
)

// CreateAutomationRuleRequest is the request body for creating an automation rule.
type CreateAutomationRuleRequest struct {
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	TriggerType   string          `json:"triggerType"`
	TriggerConfig json.RawMessage `json:"triggerConfig,omitempty"`
	Effects       json.RawMessage `json:"effects,omitempty"`
	CreatedBy     string          `json:"createdBy,omitempty"`
}

// UpdateAutomationRuleRequest is the request body for updating an automation rule.
type UpdateAutomationRuleRequest struct {
	Name          string          `json:"name,omitempty"`
	Description   string          `json:"description,omitempty"`
	TriggerType   string          `json:"triggerType,omitempty"`
	TriggerConfig json.RawMessage `json:"triggerConfig,omitempty"`
	Effects       json.RawMessage `json:"effects,omitempty"`
}

var validTriggerTypes = map[string]bool{
	"schedule":   true,
	"dataChange": true,
	"manual":     true,
}

// CreateAutomationRule handles POST /api/v2/ontologies/{ontologyApiName}/automationRules.
func (h *OMSHandler) CreateAutomationRule(w http.ResponseWriter, r *http.Request) {
	ontologyRID, err := h.resolveOntologyRID(r.Context(), chi.URLParam(r, "ontologyApiName"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("OntologyNotFound", map[string]string{
				"ontologyApiName": chi.URLParam(r, "ontologyApiName"),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ResolveOntologyFailed", nil))
		return
	}

	var req CreateAutomationRuleRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	if req.Name == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:name", map[string]string{
			"parameter": "name",
			"reason":    "name is required",
		}))
		return
	}

	if req.TriggerType == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:triggerType", map[string]string{
			"parameter": "triggerType",
			"reason":    "triggerType is required",
		}))
		return
	}

	if !validTriggerTypes[req.TriggerType] {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:triggerType", map[string]string{
			"parameter": "triggerType",
			"reason":    "triggerType must be one of: schedule, dataChange, manual",
		}))
		return
	}

	rule := &AutomationRule{
		ID:            rid.NewAutomationRuleRID(),
		OntologyRID:   ontologyRID,
		Name:          req.Name,
		Description:   req.Description,
		Status:        "active",
		TriggerType:   req.TriggerType,
		TriggerConfig: req.TriggerConfig,
		Effects:       req.Effects,
		CreatedBy:     req.CreatedBy,
	}

	if err := h.repo.CreateAutomationRule(r.Context(), rule); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("CreateAutomationRuleFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, rule)
}

// ListAutomationRules handles GET /api/v2/ontologies/{ontologyApiName}/automationRules.
func (h *OMSHandler) ListAutomationRules(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	list, err := h.repo.ListAutomationRules(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListAutomationRulesFailed", nil))
		return
	}

	// Apply optional ?status= filter
	statusFilter := r.URL.Query().Get("status")
	if statusFilter != "" {
		var filtered []AutomationRule
		for _, rule := range list {
			if rule.Status == statusFilter {
				filtered = append(filtered, rule)
			}
		}
		list = filtered
	}

	if list == nil {
		list = []AutomationRule{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": list,
	})
}

// GetAutomationRule handles GET /api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}.
func (h *OMSHandler) GetAutomationRule(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "ruleId")

	rule, err := h.repo.GetAutomationRule(r.Context(), ruleID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AutomationRuleNotFound", map[string]string{
				"ruleId": ruleID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetAutomationRuleFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, rule)
}

// UpdateAutomationRule handles PUT /api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}.
func (h *OMSHandler) UpdateAutomationRule(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "ruleId")

	var req UpdateAutomationRuleRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	existing, err := h.repo.GetAutomationRule(r.Context(), ruleID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AutomationRuleNotFound", map[string]string{
				"ruleId": ruleID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetAutomationRuleFailed", nil))
		return
	}

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Description != "" {
		existing.Description = req.Description
	}
	if req.TriggerType != "" {
		existing.TriggerType = req.TriggerType
	}
	if req.TriggerConfig != nil {
		existing.TriggerConfig = req.TriggerConfig
	}
	if req.Effects != nil {
		existing.Effects = req.Effects
	}

	if err := h.repo.UpdateAutomationRule(r.Context(), existing); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AutomationRuleNotFound", map[string]string{
				"ruleId": ruleID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UpdateAutomationRuleFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, existing)
}

// DeleteAutomationRule handles DELETE /api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}.
func (h *OMSHandler) DeleteAutomationRule(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "ruleId")

	if err := h.repo.DeleteAutomationRule(r.Context(), ruleID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AutomationRuleNotFound", map[string]string{
				"ruleId": ruleID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DeleteAutomationRuleFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PauseAutomationRule handles POST /api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/pause.
func (h *OMSHandler) PauseAutomationRule(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "ruleId")

	existing, err := h.repo.GetAutomationRule(r.Context(), ruleID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AutomationRuleNotFound", map[string]string{
				"ruleId": ruleID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetAutomationRuleFailed", nil))
		return
	}

	if existing.Status == "paused" {
		apierror.WriteJSON(w, apierror.NewConflict("AlreadyPaused", map[string]string{
			"ruleId": ruleID,
			"status": existing.Status,
		}))
		return
	}

	existing.Status = "paused"
	if err := h.repo.UpdateAutomationRule(r.Context(), existing); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("UpdateAutomationRuleFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, existing)
}

// ResumeAutomationRule handles POST /api/v2/ontologies/{ontologyApiName}/automationRules/{ruleId}/resume.
func (h *OMSHandler) ResumeAutomationRule(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "ruleId")

	existing, err := h.repo.GetAutomationRule(r.Context(), ruleID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AutomationRuleNotFound", map[string]string{
				"ruleId": ruleID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetAutomationRuleFailed", nil))
		return
	}

	if existing.Status == "active" {
		apierror.WriteJSON(w, apierror.NewConflict("AlreadyActive", map[string]string{
			"ruleId": ruleID,
			"status": existing.Status,
		}))
		return
	}

	existing.Status = "active"
	if err := h.repo.UpdateAutomationRule(r.Context(), existing); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("UpdateAutomationRuleFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, existing)
}
