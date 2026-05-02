package oms

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/functions/cache"
	"github.com/liyang/weave/pkg/functions/fnerrors"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/rid"
)

// detectFunctionCallCycle wraps DetectCallCycle so the in-flight Function's
// updated source overrides whatever the repository would return for the same
// identity. Without the override an UpdateFunction whose new body adds a
// callee that closes a cycle (A→B → publish B' adding B→A) would slip
// through because the repository still returns the old, cycle-free B.
func detectFunctionCallCycle(ctx context.Context, repo Repository, ontologyRID string, inFlight *Function) error {
	lookup := &functionCallGraphRepoOverlay{repo: repo, override: inFlight}
	return DetectCallCycle(ctx, lookup, ontologyRID, inFlight)
}

// functionCallGraphRepoOverlay routes lookups through the repository while
// returning the in-flight row whenever the requested ref matches the
// row being published. The overlay sits between the cycle detector and the
// Repository so the publish-time scan sees the new source code, not the
// persisted predecessor.
type functionCallGraphRepoOverlay struct {
	repo     Repository
	override *Function
}

func (o *functionCallGraphRepoOverlay) GetFunction(ctx context.Context, fnRID string) (*Function, error) {
	if o.override != nil && o.override.RID == fnRID && fnRID != "" {
		return o.override, nil
	}
	return o.repo.GetFunction(ctx, fnRID)
}

func (o *functionCallGraphRepoOverlay) GetFunctionByName(ctx context.Context, ontologyRID, name string) (*Function, error) {
	if o.override != nil && o.override.OntologyRID == ontologyRID && o.override.Name == name && name != "" {
		return o.override, nil
	}
	return o.repo.GetFunctionByName(ctx, ontologyRID, name)
}

func (o *functionCallGraphRepoOverlay) GetFunctionByNameVersion(ctx context.Context, ontologyRID, name, version string) (*Function, error) {
	if o.override != nil && o.override.OntologyRID == ontologyRID && o.override.Name == name &&
		o.override.NormalisedVersion() == version && name != "" {
		return o.override, nil
	}
	return o.repo.GetFunctionByNameVersion(ctx, ontologyRID, name, version)
}

// CreateFunctionRequest is the request body for creating a function. Version
// is an optional semver string (US-217); when omitted the handler defaults to
// DefaultFunctionVersion ("1.0.0"). Posting a name+version pair that already
// exists in the ontology returns 409 — new versions never overwrite older
// rows.
type CreateFunctionRequest struct {
	Name       string          `json:"name"`
	SourceCode string          `json:"sourceCode"`
	Version    string          `json:"version,omitempty"`
	Runtime    string          `json:"runtime,omitempty"`
	Signature  json.RawMessage `json:"signature,omitempty"`
	// Pure marks the function as deterministic in its inputs. When true the
	// execute handler may serve repeat calls with identical params from the
	// LRU+TTL result cache (US-221). Defaults to false when omitted so
	// nothing caches by accident.
	Pure      bool   `json:"pure,omitempty"`
	CreatedBy string `json:"createdBy,omitempty"`
}

// UpdateFunctionRequest is the request body for updating a function. Pointer
// fields distinguish "omit ⇒ preserve" from "send empty ⇒ clear" for the
// fields where that matters; bare strings keep the legacy "empty ⇒ preserve"
// semantics the original handler shipped with. Version is a semver string
// (US-217) — empty preserves the existing row's version.
type UpdateFunctionRequest struct {
	Name       string           `json:"name,omitempty"`
	SourceCode string           `json:"sourceCode,omitempty"`
	Version    string           `json:"version,omitempty"`
	Runtime    *string          `json:"runtime,omitempty"`
	Signature  *json.RawMessage `json:"signature,omitempty"`
	// Pure is a pointer so callers can distinguish "omit ⇒ preserve" from
	// "send false ⇒ disable caching" (US-221). Sending true on a previously
	// impure row opts the function into the LRU+TTL result cache the next
	// time the row is read.
	Pure *bool `json:"pure,omitempty"`
}

// CreateFunction handles POST /api/v2/ontologies/{ontologyApiName}/functions.
func (h *OMSHandler) CreateFunction(w http.ResponseWriter, r *http.Request) {
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

	var req CreateFunctionRequest
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
	if req.SourceCode == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:sourceCode", map[string]string{
			"parameter": "sourceCode",
			"reason":    "sourceCode is required",
		}))
		return
	}

	version := req.Version
	if version == "" {
		version = DefaultFunctionVersion
	}
	fn := &Function{
		RID:         rid.NewFunctionRID(),
		OntologyRID: ontologyRID,
		Name:        req.Name,
		Version:     version,
		SourceCode:  req.SourceCode,
		Runtime:     req.Runtime,
		Signature:   req.Signature,
		Pure:        req.Pure,
		CreatedBy:   req.CreatedBy,
	}
	if err := fn.Validate(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:function", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	fn.Runtime = fn.NormalisedRuntime()

	if cycleErr := detectFunctionCallCycle(r.Context(), h.repo, ontologyRID, fn); cycleErr != nil {
		var cyc *FunctionCallCycleError
		if errors.As(cycleErr, &cyc) {
			apierror.WriteJSON(w, apierror.NewFunctionCallCycle("FunctionCallCycle", map[string]string{
				"name":  fn.Name,
				"cycle": strings.Join(cyc.Cycle, " -> "),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DetectFunctionCallCycleFailed", nil))
		return
	}

	if err := h.repo.CreateFunction(r.Context(), fn); err != nil {
		if errors.Is(err, ErrDuplicate) {
			apierror.WriteJSON(w, apierror.NewConflict("FunctionAlreadyExists", map[string]string{
				"name":    req.Name,
				"version": fn.Version,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("CreateFunctionFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, fn)
}

// ListFunctions handles GET /api/v2/ontologies/{ontologyApiName}/functions.
func (h *OMSHandler) ListFunctions(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")

	list, err := h.repo.ListFunctions(r.Context(), ontologyRID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListFunctionsFailed", nil))
		return
	}

	if list == nil {
		list = []Function{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"data": list,
	})
}

// GetFunctionV2 handles GET /api/v2/ontologies/{ontologyApiName}/functions/{functionRid}.
// The path segment accepts three shapes (US-217):
//   - `<rid>` — direct lookup by RID
//   - `<name>` — latest semver of the named function
//   - `<name>@<version>` — pinned to the supplied semver
func (h *OMSHandler) GetFunctionV2(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	fnIdentifier := chi.URLParam(r, "functionRid")

	fn, err := h.resolveFunctionRef(r.Context(), ontologyRID, fnIdentifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FunctionNotFound", map[string]string{
				"functionRid": fnIdentifier,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetFunctionFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, fn)
}

// resolveFunctionRef centralises the `<rid>` / `<name>` / `<name>@<version>`
// URL-segment parsing the GetFunctionV2 + ExecuteFunction handlers share. A
// missing `@` falls back to the legacy "rid OR latest name" lookup.
func (h *OMSHandler) resolveFunctionRef(ctx context.Context, ontologyRID, ref string) (*Function, error) {
	if name, version, ok := splitFunctionRef(ref); ok {
		return h.repo.GetFunctionByNameVersion(ctx, ontologyRID, name, version)
	}
	return h.repo.GetFunctionByName(ctx, ontologyRID, ref)
}

// splitFunctionRef splits a `name@version` URL segment. Returns (name,
// version, true) when an `@` is present and both halves are non-empty;
// otherwise (zero, zero, false). RIDs never contain `@`, so the split is
// unambiguous against the existing "rid or name" paths.
func splitFunctionRef(ref string) (string, string, bool) {
	for i := 0; i < len(ref); i++ {
		if ref[i] == '@' {
			name, version := ref[:i], ref[i+1:]
			if name == "" || version == "" {
				return "", "", false
			}
			return name, version, true
		}
	}
	return "", "", false
}

// ListFunctionVersions handles GET /api/v2/ontologies/{ontologyApiName}/functions/{functionName}/versions.
// Returns every stored semver version of the named function within the
// ontology, sorted latest-first. 404 when no rows exist for the name.
func (h *OMSHandler) ListFunctionVersions(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	name := chi.URLParam(r, "functionName")

	versions, err := h.repo.ListFunctionVersionsByName(r.Context(), ontologyRID, name)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListFunctionVersionsFailed", nil))
		return
	}
	if len(versions) == 0 {
		apierror.WriteJSON(w, apierror.NewNotFound("FunctionNotFound", map[string]string{
			"name": name,
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"name": name,
		"data": versions,
	})
}

// UpdateFunction handles PUT /api/v2/ontologies/{ontologyApiName}/functions/{functionRid}.
func (h *OMSHandler) UpdateFunction(w http.ResponseWriter, r *http.Request) {
	fnRID := chi.URLParam(r, "functionRid")

	var req UpdateFunctionRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": "invalid JSON",
		}))
		return
	}

	existing, err := h.repo.GetFunction(r.Context(), fnRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FunctionNotFound", map[string]string{
				"functionRid": fnRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetFunctionFailed", nil))
		return
	}

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.SourceCode != "" {
		existing.SourceCode = req.SourceCode
	}
	if req.Version != "" {
		existing.Version = req.Version
	}
	if req.Runtime != nil {
		existing.Runtime = *req.Runtime
	}
	if req.Signature != nil {
		existing.Signature = *req.Signature
	}
	if req.Pure != nil {
		existing.Pure = *req.Pure
	}
	if err := existing.Validate(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:function", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	existing.Runtime = existing.NormalisedRuntime()

	if cycleErr := detectFunctionCallCycle(r.Context(), h.repo, existing.OntologyRID, existing); cycleErr != nil {
		var cyc *FunctionCallCycleError
		if errors.As(cycleErr, &cyc) {
			apierror.WriteJSON(w, apierror.NewFunctionCallCycle("FunctionCallCycle", map[string]string{
				"name":  existing.Name,
				"cycle": strings.Join(cyc.Cycle, " -> "),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DetectFunctionCallCycleFailed", nil))
		return
	}

	if err := h.repo.UpdateFunction(r.Context(), existing); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FunctionNotFound", map[string]string{
				"functionRid": fnRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("UpdateFunctionFailed", nil))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, existing)
}

// ExecuteFunction handles POST /api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/execute.
// The endpoint loads the function, enforces the per-realm call quota
// (US-218, HTTP 429), validates the caller's parameters against the
// declared signature (US-216), then dispatches to the optional
// FunctionExecutor under a 5s context deadline. Validation failures surface
// as 400 with a `parameter`+`code` payload; CPU-timeout / memory-limit
// violations surface as 408 / 429 respectively so SDKs can map them back
// to typed retry/backoff behaviour.
//
// US-219 streaming: when ?stream=1 is set, successful execution emits an
// NDJSON stream (Content-Type: application/x-ndjson). One newline-delimited
// JSON object per emitted item: `{"item": <value>}`. If the executor returns
// an array, each element becomes one line; a scalar result becomes one line;
// an empty array becomes zero lines. Errors that occur AFTER the executor
// has been dispatched (timeout, memory, executor failure) are emitted in-band
// as a terminal `{"error": {"code","reason"}}` line so the SDK iterator can
// surface them without parsing HTTP status codes. Pre-execution errors
// (validation, 404, quota, no-executor) still return regular HTTP error
// responses with a single JSON body — the NDJSON contract only kicks in
// once the response stream has been opened.
func (h *OMSHandler) ExecuteFunction(w http.ResponseWriter, r *http.Request) {
	fnIdentifier := chi.URLParam(r, "functionRid")
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")

	fn, err := h.resolveFunctionRef(r.Context(), ontologyAPIName, fnIdentifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FunctionNotFound", map[string]string{
				"functionRid": fnIdentifier,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetFunctionFailed", nil))
		return
	}

	// Per-realm call quota (US-218). Realm is the third segment of the
	// function's RID; fall back to "main" for legacy / malformed RIDs.
	realm := realmFromRID(fn.RID)
	if h.functionQuotaLimiter != nil && !h.functionQuotaLimiter.Allow(realm) {
		apierror.WriteJSON(w, apierror.NewTooManyRequests("FunctionQuotaExceeded", map[string]string{
			"realm":       realm,
			"functionRid": fn.RID,
		}))
		return
	}

	var body struct {
		Parameters map[string]interface{} `json:"parameters"`
	}
	// Empty bodies are legal — a function with all-default / all-optional
	// params should be invokable with no payload.
	if r.ContentLength != 0 {
		if err := httputil.ReadJSON(r, &body); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"reason": "invalid JSON",
			}))
			return
		}
	}
	if body.Parameters == nil {
		body.Parameters = map[string]interface{}{}
	}

	sig, err := ParseFunctionSignature(fn.Signature)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("InvalidStoredSignature", nil))
		return
	}
	coerced, err := ValidateAndCoerceFunctionParams(sig, body.Parameters)
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
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidParameter:parameters", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	if h.functionExecutor == nil {
		// Degraded-mode: no executor wired. Still surface the validated /
		// coerced parameter map so callers can confirm the contract is
		// honoured even when execution itself isn't available.
		w.Header().Set("X-Function-Executor", "not-configured")
		httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"functionRid": fn.RID,
			"parameters":  coerced,
			"error":       "no FunctionExecutor wired",
		})
		return
	}

	streaming := r.URL.Query().Get("stream") == "1"

	// Result cache (US-221). Pure functions short-circuit repeat calls
	// with identical params from the LRU+TTL cache. Streaming responses
	// skip the cache because the handler emits items one at a time —
	// reconstituting them from a cached scalar/slice would require an
	// extra projection layer we don't need yet. Cache misses fall through
	// to the regular dispatch path.
	cacheable := fn.Pure && !streaming && h.functionResultCache != nil
	var cacheKey string
	if cacheable {
		cacheKey = functionResultCacheKey(fn, coerced)
		if cached, hit := h.functionResultCache.Get(cacheKey); hit {
			httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
				"functionRid": fn.RID,
				"result":      cached,
				"cached":      true,
			})
			return
		}
	}

	// Handler-side CPU budget (US-218). The underlying Goja runtime also
	// enforces this ceiling via its own context watchdog, but wrapping
	// here guarantees the 5s limit applies to every FunctionExecutor
	// implementation (HTTP-backed, remote-dispatch, ...).
	execCtx, cancel := context.WithTimeout(r.Context(), DefaultFunctionExecutionTimeout)
	defer cancel()

	result, err := h.functionExecutor.Execute(execCtx, fn, coerced)
	// Persist input/output hashes for replay (US-370). Streaming responses
	// still log the executor's primary return value — the iterator yields
	// each item separately but the underlying executor result already
	// captures the materialised slice.
	h.recordFunctionExecution(r.Context(), fn, coerced, result, err, false, "")
	if streaming {
		writeFunctionStream(w, fn.RID, result, err, execCtx)
		return
	}
	if err != nil {
		// CPU timeout → 408. Either the runtime returned the typed
		// sentinel or the handler-side deadline fired without the
		// executor propagating an error that wraps it.
		if errors.Is(err, fnerrors.ErrTimeout) || errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			apierror.WriteJSON(w, apierror.NewRequestTimeout("FunctionExecutionTimeout", map[string]string{
				"functionRid": fn.RID,
				"timeout":     DefaultFunctionExecutionTimeout.String(),
			}))
			return
		}
		// Memory-limit overrun → 429. The runtime surfaces this as a
		// resource-exhausted condition; no amount of retrying the same
		// input will succeed until the function is rewritten.
		if errors.Is(err, fnerrors.ErrMemoryLimit) {
			apierror.WriteJSON(w, apierror.NewTooManyRequests("FunctionMemoryLimitExceeded", map[string]string{
				"functionRid": fn.RID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewBadRequest("FunctionExecutionFailed", map[string]string{
			"error": err.Error(),
		}))
		return
	}

	// Cache the successful result so the next pure-call with the same
	// params hits the cache. Errors are never cached — the caller may have
	// raced an external state change the function depends on.
	if cacheable {
		h.functionResultCache.Put(cacheKey, result)
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"functionRid": fn.RID,
		"result":      result,
	})
}

// writeFunctionStream emits the executor's outcome as an NDJSON stream
// (US-219). The response is opened with 200 + Content-Type:
// application/x-ndjson and either:
//   - one `{"item": <element>}` line per element when result is a slice
//   - one `{"item": <result>}` line when result is a scalar (non-nil)
//   - zero lines for an empty slice
//
// followed by an optional terminal `{"error": {"code","reason"}}` line when
// err is non-nil. The handler flushes after each line so iterating clients
// receive items as they're encoded. Errors mid-stream are in-band so the
// SDK iterator can surface them without parsing HTTP status codes.
func writeFunctionStream(w http.ResponseWriter, fnRID string, result interface{}, execErr error, execCtx context.Context) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)

	emit := func(record map[string]interface{}) {
		_ = enc.Encode(record)
		if flusher != nil {
			flusher.Flush()
		}
	}

	if execErr == nil {
		switch items := result.(type) {
		case []interface{}:
			for _, it := range items {
				emit(map[string]interface{}{"item": it})
			}
		case nil:
			// no-op — empty stream
		default:
			emit(map[string]interface{}{"item": result})
		}
		return
	}

	code := "FunctionExecutionFailed"
	switch {
	case errors.Is(execErr, fnerrors.ErrTimeout) || errors.Is(execCtx.Err(), context.DeadlineExceeded):
		code = "FunctionExecutionTimeout"
	case errors.Is(execErr, fnerrors.ErrMemoryLimit):
		code = "FunctionMemoryLimitExceeded"
	}
	emit(map[string]interface{}{
		"error": map[string]interface{}{
			"code":        code,
			"reason":      execErr.Error(),
			"functionRid": fnRID,
		},
	})
}

// functionResultCacheKey builds the canonical cache key for a Function
// invocation (US-221). The key combines the function's RID and version with
// a SHA-256 digest of the params map so two calls only collide when both
// the function build AND the input are identical. NormalisedVersion()
// substitutes the DEFAULT version when the row predates US-217 — keeps
// legacy rows cacheable without breaking the rid@version contract.
func functionResultCacheKey(fn *Function, params map[string]interface{}) string {
	return cache.Key(fn.RID, fn.NormalisedVersion(), params)
}

// realmFromRID returns the `realm` segment from a Weave Resource Identifier
// (ri.{service}.{realm}.{resourceType}.{uuid}). Falls back to "main" when
// the RID cannot be parsed — keeps quota enforcement meaningful for
// legacy rows that predate US-005 (strict RID format).
func realmFromRID(r string) string {
	parsed, err := rid.Parse(r)
	if err != nil || parsed.Realm == "" {
		return "main"
	}
	return parsed.Realm
}

// DeleteFunction handles DELETE /api/v2/ontologies/{ontologyApiName}/functions/{functionRid}.
func (h *OMSHandler) DeleteFunction(w http.ResponseWriter, r *http.Request) {
	fnRID := chi.URLParam(r, "functionRid")

	if err := h.repo.DeleteFunction(r.Context(), fnRID); err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("FunctionNotFound", map[string]string{
				"functionRid": fnRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DeleteFunctionFailed", nil))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
