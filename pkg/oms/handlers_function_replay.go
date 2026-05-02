package oms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// ReplayFunctionRequest is the wire shape for POST /functions/{rid}/replay.
// At minimum the caller specifies an executionId pointing at a previously
// persisted invocation; the handler resolves the function + version + input
// from that row and replays it. Callers may also pass an explicit version +
// input map to replay an ad-hoc tuple that was never persisted (e.g. for
// audit "what would this Function return on this input today" probes); the
// determinism check is then skipped since there is no historical hash to
// compare against.
type ReplayFunctionRequest struct {
	ExecutionID string                 `json:"executionId,omitempty"`
	Version     string                 `json:"version,omitempty"`
	Input       map[string]interface{} `json:"input,omitempty"`
}

// ReplayFunctionResponse is the wire shape returned by replay. When the
// fresh hash matches the original the response is HTTP 200 + match=true. A
// hash divergence still returns the fresh result on the body but flips
// match=false and embeds a WEAVE_FUNCTION_NONDETERMINISTIC warning under
// `warning` so SDK consumers can surface the audit notice without losing
// the replay output. Replay rows are themselves persisted with
// is_replay=true and replay_of pointing at the original execution id.
type ReplayFunctionResponse struct {
	FunctionRID     string          `json:"functionRid"`
	FunctionVersion string          `json:"functionVersion"`
	ExecutionID     string          `json:"executionId,omitempty"`
	OriginalHash    string          `json:"originalHash,omitempty"`
	ReplayHash      string          `json:"replayHash"`
	Match           bool            `json:"match"`
	Result          interface{}     `json:"result"`
	Warning         *replayWarning  `json:"warning,omitempty"`
	Original        json.RawMessage `json:"original,omitempty"`
}

type replayWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ReplayFunction handles POST /api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/replay
// (US-370). The handler resolves the function at the version pinned to the
// historical execution, re-runs it against the stored input, and compares
// the fresh output hash to the recorded hash. A divergence yields HTTP 409
// + WEAVE_FUNCTION_NONDETERMINISTIC and the replay row records the new
// hash so future audits can spot recurring drift.
func (h *OMSHandler) ReplayFunction(w http.ResponseWriter, r *http.Request) {
	if h.functionExecutor == nil {
		w.Header().Set("X-Function-Executor", "not-configured")
		httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"errorCode": "FunctionExecutorNotConfigured",
			"reason":    "no FunctionExecutor wired",
		})
		return
	}
	if h.functionExecutionStore == nil {
		httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"errorCode": "FunctionExecutionStoreNotConfigured",
			"reason":    "no FunctionExecutionStore wired",
		})
		return
	}

	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	fnIdentifier := chi.URLParam(r, "functionRid")

	var req ReplayFunctionRequest
	if r.ContentLength != 0 {
		if err := httputil.ReadJSON(r, &req); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"reason": "invalid JSON",
			}))
			return
		}
	}

	var (
		original  *FunctionExecution
		input     map[string]interface{}
		version   string
	)
	if req.ExecutionID != "" {
		got, err := h.functionExecutionStore.GetExecution(r.Context(), req.ExecutionID)
		if err != nil {
			if errors.Is(err, ErrExecutionNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("FunctionExecutionNotFound", map[string]string{
					"executionId": req.ExecutionID,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInternal("GetFunctionExecutionFailed", nil))
			return
		}
		original = got
		version = got.FunctionVersion
		if err := json.Unmarshal(got.InputJSON, &input); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("InvalidStoredInput", nil))
			return
		}
		if input == nil {
			input = map[string]interface{}{}
		}
	} else {
		if req.Version == "" {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:version", map[string]string{
				"parameter": "version",
				"reason":    "version is required when executionId is omitted",
			}))
			return
		}
		version = req.Version
		input = req.Input
		if input == nil {
			input = map[string]interface{}{}
		}
	}

	fn, err := h.resolveFunctionForReplay(r.Context(), ontologyAPIName, fnIdentifier, version)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FunctionNotFound", map[string]string{
				"functionRid": fnIdentifier,
				"version":     version,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetFunctionFailed", nil))
		return
	}

	sig, err := ParseFunctionSignature(fn.Signature)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("InvalidStoredSignature", nil))
		return
	}
	coerced, err := ValidateAndCoerceFunctionParams(sig, input)
	if err != nil {
		var pe *FunctionParamError
		if errors.As(err, &pe) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:"+pe.Parameter, map[string]string{
				"parameter": pe.Parameter,
				"code":      pe.Code,
				"reason":    pe.Reason,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:input", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	execCtx, cancel := context.WithTimeout(r.Context(), DefaultFunctionExecutionTimeout)
	defer cancel()
	result, execErr := h.functionExecutor.Execute(execCtx, fn, coerced)

	originalID := ""
	if original != nil {
		originalID = original.ExecutionID
	}
	row := h.recordFunctionExecution(r.Context(), fn, coerced, result, execErr, true, originalID)

	if execErr != nil {
		apierror.WriteJSON(w, apierror.NewBadRequest("FunctionExecutionFailed", map[string]string{
			"functionRid": fn.RID,
			"error":       execErr.Error(),
		}))
		return
	}

	replayHash := HashFunctionOutput(result)
	if row != nil && row.OutputHash != "" {
		replayHash = row.OutputHash
	}

	resp := ReplayFunctionResponse{
		FunctionRID:     fn.RID,
		FunctionVersion: fn.NormalisedVersion(),
		ReplayHash:      replayHash,
		Match:           true,
		Result:          result,
	}
	if row != nil {
		resp.ExecutionID = row.ExecutionID
	}

	if original != nil {
		resp.OriginalHash = original.OutputHash
		if original.OutputHash != "" && original.OutputHash != replayHash {
			resp.Match = false
			resp.Warning = &replayWarning{
				Code: "WEAVE_FUNCTION_NONDETERMINISTIC",
				Message: fmt.Sprintf(
					"replay output hash %s diverges from the recorded hash %s",
					replayHash, original.OutputHash,
				),
			}
			if originalRaw, err := json.Marshal(original); err == nil {
				resp.Original = originalRaw
			}
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// resolveFunctionForReplay loads the row that backed the original execution.
// When `version` is empty the handler falls back to the legacy `<rid>` /
// `<name>` resolution path so callers can replay against the latest build.
// A non-empty version pins to (name, version) — the registry's notion of a
// frozen build at publish time.
func (h *OMSHandler) resolveFunctionForReplay(ctx context.Context, ontologyAPIName, ref, version string) (*Function, error) {
	if version == "" {
		return h.resolveFunctionRef(ctx, ontologyAPIName, ref)
	}
	if name, _, ok := splitFunctionRef(ref); ok {
		return h.repo.GetFunctionByNameVersion(ctx, ontologyAPIName, name, version)
	}
	if fn, err := h.repo.GetFunctionByNameVersion(ctx, ontologyAPIName, ref, version); err == nil {
		return fn, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	return h.repo.GetFunction(ctx, ref)
}

// recordFunctionExecution persists a Function invocation to the audit log
// (US-370). The row captures the input/output hashes so /replay can detect
// non-determinism. A nil store, marshalling failure, or duplicate row
// silently skip — execute itself never fails because of a logging miss.
// Returns the persisted row (or nil when persistence was skipped) so the
// caller can echo the executionId back to the SDK.
func (h *OMSHandler) recordFunctionExecution(ctx context.Context, fn *Function, params map[string]interface{}, result interface{}, execErr error, isReplay bool, replayOf string) *FunctionExecution {
	if h.functionExecutionStore == nil {
		return nil
	}
	if params == nil {
		params = map[string]interface{}{}
	}
	inputHash := HashFunctionInput(params)
	outputHash := ""
	errMsg := ""
	if execErr == nil {
		outputHash = HashFunctionOutput(result)
	} else {
		errMsg = execErr.Error()
	}
	inputJSON, err := json.Marshal(params)
	if err != nil {
		inputJSON = []byte(`{}`)
	}
	outputJSON := []byte(`null`)
	if execErr == nil {
		if raw, err := json.Marshal(result); err == nil {
			outputJSON = raw
		}
	}
	now := time.Now().UTC()
	row := &FunctionExecution{
		ExecutionID:     NewFunctionExecutionID(fn.RID, fn.NormalisedVersion(), inputHash, now),
		FunctionRID:     fn.RID,
		FunctionName:    fn.Name,
		FunctionVersion: fn.NormalisedVersion(),
		OntologyRID:     fn.OntologyRID,
		InputHash:       inputHash,
		OutputHash:      outputHash,
		InputJSON:       inputJSON,
		OutputJSON:      outputJSON,
		ErrorMessage:    errMsg,
		IsReplay:        isReplay,
		ReplayOf:        replayOf,
		ExecutedAt:      now,
	}
	if h.actorFn != nil {
		row.RequestedBy = h.actorFn(ctx)
	}
	if err := h.functionExecutionStore.RecordExecution(ctx, row); err != nil {
		return nil
	}
	return row
}
