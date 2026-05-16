// Package cliclient is a thin HTTP client wrapper around the Weave REST API
// designed for use by command-line tools and small Go integrations.
//
// It is intentionally minimal — it covers the read-mostly paths the `weave`
// CLI needs (auth, ontology metadata, object retrieval, action apply) and
// returns plain Go structs that mirror the wire format. For richer features
// such as composable ObjectSets, callers should use the Python SDK or talk to
// the HTTP API directly.
package cliclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to a Weave server. The zero value is not usable; construct one
// via NewClient.
type Client struct {
	BaseURL string
	Token   string // JWT access token or wvk_-prefixed API key.
	HTTP    *http.Client
}

// NewClient builds a Client. baseURL may end in a trailing slash, which is
// stripped. If token is empty no Authorization header is sent (useful for
// /api/auth/login). The default timeout is 30 seconds.
func NewClient(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// APIError is the typed error returned for any non-2xx response.
type APIError struct {
	StatusCode      int               `json:"-"`
	ErrorCode       string            `json:"errorCode"`
	ErrorName       string            `json:"errorName"`
	ErrorInstanceID string            `json:"errorInstanceId"`
	Parameters      map[string]string `json:"parameters"`
	RawBody         string            `json:"-"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.ErrorName != "" {
		return fmt.Sprintf("weave: %d %s/%s", e.StatusCode, e.ErrorCode, e.ErrorName)
	}
	return fmt.Sprintf("weave: %d %s", e.StatusCode, e.RawBody)
}

// IsNotFound reports whether err is a 404 APIError.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}

// IsUnauthorized reports whether err is a 401 APIError.
func IsUnauthorized(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusUnauthorized
	}
	return false
}

// Ontology mirrors api.openapi.yaml#/components/schemas/Ontology.
type Ontology struct {
	RID            string `json:"rid"`
	APIName        string `json:"apiName"`
	DisplayName    string `json:"displayName"`
	Description    string `json:"description,omitempty"`
	CurrentVersion int    `json:"currentVersion"`
}

// ObjectType is the wire ObjectType payload — properties are intentionally
// loose to avoid coupling the CLI to the full property data-type tree.
type ObjectType struct {
	RID               string         `json:"rid"`
	APIName           string         `json:"apiName"`
	DisplayName       string         `json:"displayName"`
	PluralDisplayName string         `json:"pluralDisplayName,omitempty"`
	Description       string         `json:"description,omitempty"`
	PrimaryKey        string         `json:"primaryKey"`
	TitleProperty     string         `json:"titleProperty,omitempty"`
	Status            string         `json:"status"`
	Visibility        string         `json:"visibility"`
	Properties        map[string]any `json:"properties,omitempty"`
}

// LinkType mirrors LinkType from the spec.
type LinkType struct {
	RID                     string `json:"rid"`
	APIName                 string `json:"apiName"`
	DisplayName             string `json:"displayName"`
	ObjectTypeAPIName       string `json:"objectTypeApiName"`
	LinkedObjectTypeAPIName string `json:"linkedObjectTypeApiName"`
	Cardinality             string `json:"cardinality"`
	Required                bool   `json:"required"`
}

// ActionType mirrors ActionType from the spec.
type ActionType struct {
	RID         string         `json:"rid"`
	APIName     string         `json:"apiName"`
	DisplayName string         `json:"displayName"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"status"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// WireObject is the flattened V2 object payload (properties at the top level
// alongside __rid / __primaryKey / __apiName).
type WireObject map[string]any

// ObjectPage is the response shape for listObjects / searchObjects.
type ObjectPage struct {
	Data          []WireObject `json:"data"`
	NextPageToken string       `json:"nextPageToken,omitempty"`
	TotalCount    string       `json:"totalCount,omitempty"`
}

// ListObjectsOptions controls listObjects pagination/order.
type ListObjectsOptions struct {
	PageSize  int
	PageToken string
	OrderBy   string
}

// ActionResultsCLI mirrors the Foundry OSv2 ActionResults (edit summary).
type ActionResultsCLI struct {
	Type                string `json:"type"`
	AddedObjectCount    int    `json:"addedObjectCount"`
	ModifiedObjectCount int    `json:"modifiedObjectCount"`
	DeletedObjectCount  int    `json:"deletedObjectCount"`
	AddedLinksCount     int    `json:"addedLinksCount"`
	DeletedLinksCount   int    `json:"deletedLinksCount"`
}

// ApplyOptions controls the behaviour of ApplyAction (validation mode, edit
// return policy). A nil value means the server defaults are used.
type ApplyOptions struct {
	Mode        string `json:"mode,omitempty"`
	ReturnEdits string `json:"returnEdits,omitempty"`
}

// ValidationResult mirrors the Foundry OSv2 ValidationResult envelope.
type ValidationResult struct {
	Result string `json:"result"`
}

// ApplyActionResponse mirrors the Foundry OSv2 SyncApplyActionResponseV2.
type ApplyActionResponse struct {
	OperationID string            `json:"operationId,omitempty"`
	Validation  *ValidationResult `json:"validation,omitempty"`
	Edits       *ActionResultsCLI `json:"edits,omitempty"`
}

// BatchApplyResponse mirrors the Foundry OSv2 BatchApplyActionResponseV2.
type BatchApplyResponse struct {
	Edits *ActionResultsCLI `json:"edits,omitempty"`
}

// InterfaceType mirrors the spec.
type InterfaceType struct {
	RID         string `json:"rid"`
	APIName     string `json:"apiName"`
	DisplayName string `json:"displayName"`
}

// ValueType mirrors the spec.
type ValueType struct {
	RID         string `json:"rid"`
	APIName     string `json:"apiName"`
	DisplayName string `json:"displayName"`
	BaseType    string `json:"baseType"`
	Version     int    `json:"version"`
}

// QueryType mirrors the spec.
type QueryType struct {
	RID         string         `json:"rid"`
	APIName     string         `json:"apiName"`
	DisplayName string         `json:"displayName"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"status"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Output      map[string]any `json:"output,omitempty"`
}

// LoginResponse mirrors the login response payload.
type LoginResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	User         LoginUser `json:"user"`
}

// LoginUser mirrors the user payload nested in LoginResponse.
type LoginUser struct {
	ID            string            `json:"id"`
	Email         string            `json:"email"`
	Name          string            `json:"name"`
	Roles         []string          `json:"roles"`
	OntologyRoles map[string]string `json:"ontologyRoles"`
}

// ----- generic request helpers ---------------------------------------------

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || len(respBody) == 0 {
			return nil
		}
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		return nil
	}

	apiErr := &APIError{StatusCode: resp.StatusCode, RawBody: string(respBody)}
	// Best effort to parse the structured envelope.
	_ = json.Unmarshal(respBody, apiErr)
	return apiErr
}

// ----- Ontology endpoints --------------------------------------------------

// ListOntologies returns all ontologies the caller can see.
func (c *Client) ListOntologies(ctx context.Context) ([]Ontology, error) {
	var resp struct {
		Data []Ontology `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v2/ontologies", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetOntology fetches a single ontology by API name (or RID).
func (c *Client) GetOntology(ctx context.Context, apiName string) (*Ontology, error) {
	var o Ontology
	if err := c.do(ctx, http.MethodGet, "/api/v2/ontologies/"+url.PathEscape(apiName), nil, &o); err != nil {
		return nil, err
	}
	return &o, nil
}

// ListObjectTypes lists object types in an ontology.
func (c *Client) ListObjectTypes(ctx context.Context, ontology string) ([]ObjectType, error) {
	var resp struct {
		Data []ObjectType `json:"data"`
	}
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/objectTypes"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetObjectType fetches a single object type wire payload.
func (c *Client) GetObjectType(ctx context.Context, ontology, objectType string) (*ObjectType, error) {
	var ot ObjectType
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/objectTypes/" + url.PathEscape(objectType)
	if err := c.do(ctx, http.MethodGet, path, nil, &ot); err != nil {
		return nil, err
	}
	return &ot, nil
}

// ListActionTypes lists action types in an ontology.
func (c *Client) ListActionTypes(ctx context.Context, ontology string) ([]ActionType, error) {
	var resp struct {
		Data []ActionType `json:"data"`
	}
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/actionTypes"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// ----- Object endpoints ----------------------------------------------------

// ListObjects fetches one page of objects.
func (c *Client) ListObjects(ctx context.Context, ontology, objectType string, opts ListObjectsOptions) (*ObjectPage, error) {
	q := url.Values{}
	if opts.PageSize > 0 {
		q.Set("pageSize", strconv.Itoa(opts.PageSize))
	}
	if opts.PageToken != "" {
		q.Set("pageToken", opts.PageToken)
	}
	if opts.OrderBy != "" {
		q.Set("orderBy", opts.OrderBy)
	}
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/objects/" + url.PathEscape(objectType)
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var page ObjectPage
	if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// GetObject fetches a single object by primary key.
func (c *Client) GetObject(ctx context.Context, ontology, objectType, primaryKey string) (WireObject, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/objects/" + url.PathEscape(objectType) +
		"/" + url.PathEscape(primaryKey)
	var obj WireObject
	if err := c.do(ctx, http.MethodGet, path, nil, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// SearchObjects POSTs a where-clause search request. The selectProps argument
// lists the property apiNames to return (required by the server per US-016).
func (c *Client) SearchObjects(ctx context.Context, ontology, objectType string, where map[string]any, selectProps []string) (*ObjectPage, error) {
	body := map[string]any{"where": where, "select": selectProps}
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/objects/" + url.PathEscape(objectType) + "/search"
	var page ObjectPage
	if err := c.do(ctx, http.MethodPost, path, body, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// ----- Action endpoints ----------------------------------------------------

// ApplyAction submits a single action and returns the resulting edits.
// The action API name is carried in the URL (Foundry OSv2 shape); only
// the parameters (and optional options) travel in the request body.
func (c *Client) ApplyAction(ctx context.Context, ontology, actionType string, parameters map[string]any, opts *ApplyOptions) (*ApplyActionResponse, error) {
	body := map[string]any{
		"parameters": parameters,
	}
	if opts != nil {
		body["options"] = opts
	}
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/actions/" + url.PathEscape(actionType) + "/apply"
	var resp ApplyActionResponse
	if err := c.do(ctx, http.MethodPost, path, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ----- Ontology metadata endpoints ----------------------------------------

// LoadOntologyMetadata loads a subset of ontology metadata (e.g. objectTypes,
// actionTypes) by POSTing the desired subset keys.
func (c *Client) LoadOntologyMetadata(ctx context.Context, ontology string, subsets map[string]bool) (map[string]any, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/metadata"
	var resp map[string]any
	if err := c.do(ctx, http.MethodPost, path, subsets, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// GetOntologyFullMetadata returns the full metadata for an ontology (preview).
func (c *Client) GetOntologyFullMetadata(ctx context.Context, ontology string) (map[string]any, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/fullMetadata?preview=true"
	var resp map[string]any
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ExportOntology returns the full ontology export envelope produced by
// GET /api/v2/ontologies/{name}/export. The shape mirrors pkg/oms.OntologyExport
// — every entity collection is non-nil even when empty so downstream callers
// (weave-cli pkg export) can range over them without nil guards.
func (c *Client) ExportOntology(ctx context.Context, ontology string) (map[string]any, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/export"
	var resp map[string]any
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// PostInstallPackage POSTs the parsed contents of a .weavepkg archive to
// /api/v2/pkg/install (US-412). The body shape is the wire mirror of
// pkg/oms.PackageInstallRequest — manifest + ontology JSON + migrations +
// onConflict knob — and the response is the loose map shape of
// pkg/oms.PackageInstallResponse.
//
// 409 PackageConflict is the conflict path the CLI surfaces with a tailored
// error message; this method just returns the typed APIError verbatim and
// lets the caller render.
func (c *Client) PostInstallPackage(ctx context.Context, body map[string]any) (map[string]any, error) {
	var resp map[string]any
	if err := c.do(ctx, http.MethodPost, "/api/v2/pkg/install", body, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ListInstalledPackages returns every row in the installed_packages
// registry (US-412 / US-413). The wire shape is `{data: [...]}` and the
// returned slice is the unwrapped data array.
func (c *Client) ListInstalledPackages(ctx context.Context) ([]map[string]any, error) {
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v2/pkg", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// ----- ObjectType extended endpoints --------------------------------------

// GetObjectTypeFullMetadata returns the full metadata for an object type (preview).
func (c *Client) GetObjectTypeFullMetadata(ctx context.Context, ontology, objectType string) (map[string]any, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/objectTypes/" + url.PathEscape(objectType) + "/fullMetadata?preview=true"
	var resp map[string]any
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// GetObjectTypesByRidBatch fetches multiple object types by RID in a single request.
func (c *Client) GetObjectTypesByRidBatch(ctx context.Context, ontology string, rids []string) ([]map[string]any, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/objectTypes/getByRidBatch"
	body := map[string]any{"rids": rids}
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, path, body, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// ----- ActionType extended endpoints --------------------------------------

// GetActionType fetches a single action type by API name.
func (c *Client) GetActionType(ctx context.Context, ontology, actionType string) (*ActionType, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/actionTypes/" + url.PathEscape(actionType)
	var at ActionType
	if err := c.do(ctx, http.MethodGet, path, nil, &at); err != nil {
		return nil, err
	}
	return &at, nil
}

// GetActionTypeByRid fetches a single action type by its RID.
func (c *Client) GetActionTypeByRid(ctx context.Context, ontology, rid string) (*ActionType, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/actionTypes/byRid/" + url.PathEscape(rid)
	var at ActionType
	if err := c.do(ctx, http.MethodGet, path, nil, &at); err != nil {
		return nil, err
	}
	return &at, nil
}

// GetActionTypesByRidBatch fetches multiple action types by RID in a single request.
func (c *Client) GetActionTypesByRidBatch(ctx context.Context, ontology string, rids []string) ([]map[string]any, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/actionTypes/getByRidBatch"
	body := map[string]any{"rids": rids}
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, path, body, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetActionTypeFullMetadata returns the full metadata for an action type (preview).
func (c *Client) GetActionTypeFullMetadata(ctx context.Context, ontology, actionType string) (map[string]any, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/actionTypes/" + url.PathEscape(actionType) + "/fullMetadata?preview=true"
	var resp map[string]any
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ListActionTypesFullMetadata lists all action types with full metadata (preview).
func (c *Client) ListActionTypesFullMetadata(ctx context.Context, ontology string) ([]map[string]any, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/actionTypesFullMetadata?preview=true"
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// ----- Object extended endpoints ------------------------------------------

// CountObjects returns the number of objects of a given type.
func (c *Client) CountObjects(ctx context.Context, ontology, objectType string) (int, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/objects/" + url.PathEscape(objectType) + "/count"
	var resp struct {
		Count int `json:"count"`
	}
	if err := c.do(ctx, http.MethodPost, path, map[string]any{}, &resp); err != nil {
		return 0, err
	}
	return resp.Count, nil
}

// ListLinkedObjects returns one page of objects linked through the given link type.
func (c *Client) ListLinkedObjects(ctx context.Context, ontology, objectType, primaryKey, linkType string, opts ListObjectsOptions) (*ObjectPage, error) {
	q := url.Values{}
	if opts.PageSize > 0 {
		q.Set("pageSize", strconv.Itoa(opts.PageSize))
	}
	if opts.PageToken != "" {
		q.Set("pageToken", opts.PageToken)
	}
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/objects/" + url.PathEscape(objectType) +
		"/" + url.PathEscape(primaryKey) +
		"/links/" + url.PathEscape(linkType)
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var page ObjectPage
	if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// GetLinkedObject fetches a specific linked object by its primary key.
func (c *Client) GetLinkedObject(ctx context.Context, ontology, objectType, primaryKey, linkType, linkedPK string) (WireObject, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/objects/" + url.PathEscape(objectType) +
		"/" + url.PathEscape(primaryKey) +
		"/links/" + url.PathEscape(linkType) +
		"/" + url.PathEscape(linkedPK)
	var obj WireObject
	if err := c.do(ctx, http.MethodGet, path, nil, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// ----- Batch action endpoints ---------------------------------------------

// ApplyBatch submits a batch of action requests and optionally returns edit results.
func (c *Client) ApplyBatch(ctx context.Context, ontology, actionType string, requests []map[string]any, returnEdits string) (*BatchApplyResponse, error) {
	body := map[string]any{
		"requests": requests,
		"options":  map[string]any{"returnEdits": returnEdits},
	}
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/actions/" + url.PathEscape(actionType) + "/applyBatch"
	var resp BatchApplyResponse
	if err := c.do(ctx, http.MethodPost, path, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ----- Interface, ValueType, QueryType endpoints --------------------------

// ListInterfaceTypes returns all interface types in an ontology (preview).
func (c *Client) ListInterfaceTypes(ctx context.Context, ontology string) ([]InterfaceType, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/interfaceTypes?preview=true"
	var resp struct {
		Data []InterfaceType `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetInterfaceType fetches a single interface type by API name.
func (c *Client) GetInterfaceType(ctx context.Context, ontology, interfaceType string) (*InterfaceType, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/interfaceTypes/" + url.PathEscape(interfaceType)
	var it InterfaceType
	if err := c.do(ctx, http.MethodGet, path, nil, &it); err != nil {
		return nil, err
	}
	return &it, nil
}

// ListValueTypes returns all value types in an ontology (preview).
func (c *Client) ListValueTypes(ctx context.Context, ontology string) ([]ValueType, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/valueTypes?preview=true"
	var resp struct {
		Data []ValueType `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetValueType fetches a single value type by API name.
func (c *Client) GetValueType(ctx context.Context, ontology, valueType string) (*ValueType, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/valueTypes/" + url.PathEscape(valueType)
	var vt ValueType
	if err := c.do(ctx, http.MethodGet, path, nil, &vt); err != nil {
		return nil, err
	}
	return &vt, nil
}

// ListQueryTypes returns all query types in an ontology.
func (c *Client) ListQueryTypes(ctx context.Context, ontology string) ([]QueryType, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/queryTypes"
	var resp struct {
		Data []QueryType `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetQueryType fetches a single query type by API name.
func (c *Client) GetQueryType(ctx context.Context, ontology, queryType string) (*QueryType, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/queryTypes/" + url.PathEscape(queryType)
	var qt QueryType
	if err := c.do(ctx, http.MethodGet, path, nil, &qt); err != nil {
		return nil, err
	}
	return &qt, nil
}

// ExecuteQuery runs a query with the given parameters and returns the raw result.
func (c *Client) ExecuteQuery(ctx context.Context, ontology, queryAPIName string, parameters map[string]any) (map[string]any, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/queries/" + url.PathEscape(queryAPIName) + "/execute"
	body := map[string]any{"parameters": parameters}
	var resp map[string]any
	if err := c.do(ctx, http.MethodPost, path, body, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ----- ObjectSet endpoints ------------------------------------------------

// LoadObjectSetObjects loads objects from an object set definition with optional
// pagination and property selection.
func (c *Client) LoadObjectSetObjects(ctx context.Context, ontology string, objectSet map[string]any, selectProps []string, pageSize int, pageToken string) (*ObjectPage, error) {
	body := map[string]any{"objectSet": objectSet, "select": selectProps}
	if pageSize > 0 {
		body["pageSize"] = pageSize
	}
	if pageToken != "" {
		body["pageToken"] = pageToken
	}
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/objectSets/loadObjects"
	var page ObjectPage
	if err := c.do(ctx, http.MethodPost, path, body, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// LoadObjectSetLinks loads linked objects from an object set by link type.
func (c *Client) LoadObjectSetLinks(ctx context.Context, ontology string, objectSet map[string]any, linkType string, selectProps []string) (*ObjectPage, error) {
	body := map[string]any{
		"objectSet": objectSet,
		"linkType":  linkType,
		"select":    selectProps,
	}
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/objectSets/loadLinks"
	var page ObjectPage
	if err := c.do(ctx, http.MethodPost, path, body, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// AggregateObjectSet runs an aggregation over an object set.
func (c *Client) AggregateObjectSet(ctx context.Context, ontology string, objectSet, aggregation map[string]any) (map[string]any, error) {
	body := map[string]any{
		"objectSet":   objectSet,
		"aggregation": aggregation,
	}
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/objectSets/aggregate"
	var resp map[string]any
	if err := c.do(ctx, http.MethodPost, path, body, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// CreateTemporaryObjectSet creates a temporary object set and returns its RID.
func (c *Client) CreateTemporaryObjectSet(ctx context.Context, ontology string, objectSet map[string]any) (string, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/objectSets/createTemporary"
	var resp struct {
		ObjectSetRid string `json:"objectSetRid"`
	}
	if err := c.do(ctx, http.MethodPost, path, objectSet, &resp); err != nil {
		return "", err
	}
	return resp.ObjectSetRid, nil
}

// GetObjectSet retrieves a previously-created object set by its RID.
func (c *Client) GetObjectSet(ctx context.Context, ontology, objectSetRid string) (map[string]any, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/objectSets/" + url.PathEscape(objectSetRid)
	var resp map[string]any
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ----- Admin endpoints -----------------------------------------------------

// RebuildIndexResponse is the reply shape of
// POST /api/admin/indexes/rebuild.
type RebuildIndexResponse struct {
	ScopedKey    string `json:"scopedKey"`
	IndexedCount int    `json:"indexedCount"`
}

// RebuildIndex asks the server to delete, recreate, and reindex the Bleve
// index for (ontology, objectType) from the authoritative object_history
// tail. The caller must hold an admin-level token; the server returns 403
// otherwise. Returns the resulting scoped key and count of indexed
// documents on success.
func (c *Client) RebuildIndex(ctx context.Context, ontology, objectType string) (*RebuildIndexResponse, error) {
	body := map[string]string{
		"ontology":   ontology,
		"objectType": objectType,
	}
	var resp RebuildIndexResponse
	if err := c.do(ctx, http.MethodPost, "/api/admin/indexes/rebuild", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DatasetTransaction mirrors oms.DatasetTransaction's wire shape. The CLI
// only surfaces the bookkeeping fields needed to render the post-rollback
// summary; richer fields can be added without breaking callers because the
// JSON decoder ignores unknown keys.
type DatasetTransaction struct {
	TxID             string    `json:"txId"`
	ParentTxID       string    `json:"parentTxId,omitempty"`
	OntologyAPIName  string    `json:"ontologyApiName"`
	CommittedAt      time.Time `json:"committedAt"`
	EditsCount       int       `json:"editsCount"`
	UserID           string    `json:"userId,omitempty"`
	RolledBackAt     time.Time `json:"rolledBackAt,omitempty"`
	RolledBackToTxID string    `json:"rolledBackToTxId,omitempty"`
}

// CreateDatasetTransactionRequest is the optional body for POST
// /api/v2/datasets/{rid}/transactions. Both fields are optional — an empty
// request creates a default checkpoint stamped against the prior chain
// head.
type CreateDatasetTransactionRequest struct {
	UserID     string `json:"userId,omitempty"`
	EditsCount int    `json:"editsCount,omitempty"`
}

// CreateDatasetTransaction stamps an explicit checkpoint into the
// dataset_transactions chain so a caller can pin a future ?asOf=tx-... or
// rollback target to a known point. Returns the freshly-recorded row.
func (c *Client) CreateDatasetTransaction(ctx context.Context, datasetRID string, req *CreateDatasetTransactionRequest) (*DatasetTransaction, error) {
	path := "/api/v2/datasets/" + url.PathEscape(datasetRID) + "/transactions"
	var body any
	if req != nil {
		body = req
	}
	var tx DatasetTransaction
	if err := c.do(ctx, http.MethodPost, path, body, &tx); err != nil {
		return nil, err
	}
	return &tx, nil
}

// PITRRollbackResponse mirrors the wire shape of POST
// /api/v2/datasets/{rid}/rollback. The server may degrade the per-PK
// replay (no affectedStore / historyStore / index manager wired) — in that
// case RestoredObjects + DeletedObjects are zero and the response carries
// only the audit overlay (RolledBackTxIDs + NewTransaction).
type PITRRollbackResponse struct {
	RolledBackTxIDs []string            `json:"rolledBackTxIds"`
	RestoredObjects int                 `json:"restoredObjects"`
	DeletedObjects  int                 `json:"deletedObjects"`
	NewTransaction  *DatasetTransaction `json:"newTransaction,omitempty"`
	TargetTx        *DatasetTransaction `json:"targetTx,omitempty"`
}

// RollbackDataset triggers point-in-time recovery: every dataset
// transaction strictly newer than `targetTxID` is marked rolled-back, every
// affected (objectType, primaryKey) pair has its live Bleve doc replayed
// against the snapshot at the target's CommittedAt (restore prior state, or
// delete if the row did not exist at the target), and a fresh bookkeeping
// transaction is recorded as the new chain head.
//
// `targetTxID` MUST start with "tx-"; the server rejects other inputs with
// InvalidRollbackTarget.
func (c *Client) RollbackDataset(ctx context.Context, datasetRID, targetTxID string) (*PITRRollbackResponse, error) {
	path := "/api/v2/datasets/" + url.PathEscape(datasetRID) +
		"/rollback?to=" + url.QueryEscape(targetTxID)
	var resp PITRRollbackResponse
	if err := c.do(ctx, http.MethodPost, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DatasetHistoryResponse mirrors GET /api/v2/datasets/{rid}/history.
type DatasetHistoryResponse struct {
	Transactions []DatasetTransaction `json:"transactions"`
}

// DatasetHistory returns the per-ontology transaction chain for a dataset,
// newest first. The server caps the response at 1000 rows.
func (c *Client) DatasetHistory(ctx context.Context, datasetRID string) (*DatasetHistoryResponse, error) {
	path := "/api/v2/datasets/" + url.PathEscape(datasetRID) + "/history"
	var resp DatasetHistoryResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ----- Auth endpoints ------------------------------------------------------

// Login exchanges email + password for an access/refresh token pair. The
// returned tokens are not stored on the Client; callers are expected to
// persist them and rebuild a new Client with the access token.
func (c *Client) Login(ctx context.Context, email, password string) (*LoginResponse, error) {
	body := map[string]any{"email": email, "password": password}
	// /api/auth/login is unauthenticated — temporarily clear the token
	// without mutating the receiver.
	tmp := *c
	tmp.Token = ""
	var resp LoginResponse
	if err := tmp.do(ctx, http.MethodPost, "/api/auth/login", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Logout revokes a refresh token. The server always returns 204; this method
// returns nil on success.
func (c *Client) Logout(ctx context.Context, refreshToken string) error {
	body := map[string]any{"refresh_token": refreshToken}
	return c.do(ctx, http.MethodPost, "/api/auth/logout", body, nil)
}

// ----- Function endpoints --------------------------------------------------

// Function mirrors the wire payload for a single Function row. The fields
// are intentionally narrow (only what `weave fn pull/push` reads) so this
// stays decoupled from the much wider pkg/oms.Function struct.
type Function struct {
	RID        string `json:"rid"`
	Name       string `json:"name"`
	Version    string `json:"version,omitempty"`
	SourceCode string `json:"sourceCode"`
	Runtime    string `json:"runtime,omitempty"`
}

// FunctionRepoCommit mirrors the wire payload returned by the
// /functions/{rid}/commits and /log endpoints (US-415).
type FunctionRepoCommit struct {
	Hash       string    `json:"hash"`
	Message    string    `json:"message"`
	Author     string    `json:"author"`
	Email      string    `json:"email"`
	AuthorDate time.Time `json:"authorDate"`
}

// GetFunction fetches a single Function row, accepting `rid`, `name`, or
// `name@version` as the identifier (the server resolves the form).
func (c *Client) GetFunction(ctx context.Context, ontology, ref string) (*Function, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/functions/" + url.PathEscape(ref)
	var fn Function
	if err := c.do(ctx, http.MethodGet, path, nil, &fn); err != nil {
		return nil, err
	}
	return &fn, nil
}

// CreateFunctionRepoCommit posts a new commit to the per-Function bare git
// repo (US-415). Either `sourceCode` or `patch` must be supplied; the
// server treats `patch` as an alias for `sourceCode`.
func (c *Client) CreateFunctionRepoCommit(ctx context.Context, ontology, ref string, body map[string]any) (*FunctionRepoCommit, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/functions/" + url.PathEscape(ref) + "/commits"
	var commit FunctionRepoCommit
	if err := c.do(ctx, http.MethodPost, path, body, &commit); err != nil {
		return nil, err
	}
	return &commit, nil
}

// ListFunctionRepoCommits fetches the newest-first commit list for the
// Function's bare git repo (US-415). When `limit > 0` the server caps the
// response.
func (c *Client) ListFunctionRepoCommits(ctx context.Context, ontology, ref string, limit int) ([]FunctionRepoCommit, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/functions/" + url.PathEscape(ref) + "/log"
	if limit > 0 {
		path += "?limit=" + strconv.Itoa(limit)
	}
	var resp struct {
		Data []FunctionRepoCommit `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// ----- OSV2-304 generic POST helpers --------------------------------------

// PostJSONRaw POSTs a raw JSON body and returns the raw JSON response. It is
// the escape hatch for the OSV2-304 CLI subcommands (aggregate / objectset
// load / objectset createTemporary) that want to forward user-authored JSON
// verbatim without coupling cliclient to every wire schema.
//
// The body is forwarded byte-for-byte (only its Content-Type and Accept
// headers are set). On non-2xx responses the returned error is a *APIError.
func (c *Client) PostJSONRaw(ctx context.Context, path string, body json.RawMessage) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return json.RawMessage(respBody), nil
	}
	apiErr := &APIError{StatusCode: resp.StatusCode, RawBody: string(respBody)}
	_ = json.Unmarshal(respBody, apiErr)
	return nil, apiErr
}

// AggregateObjects POSTs the supplied aggregation request to
// /api/v2/ontologies/{ontology}/objects/{objectType}/aggregate and returns
// the raw JSON response so the CLI can present it either as pretty JSON or
// as a flat key/value table (the metric layouts vary too much to typecast).
func (c *Client) AggregateObjects(ctx context.Context, ontology, objectType string, body json.RawMessage) (json.RawMessage, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) +
		"/objects/" + url.PathEscape(objectType) + "/aggregate"
	return c.PostJSONRaw(ctx, path, body)
}

// LoadObjectSet POSTs an ObjectSet load request and returns the raw JSON
// response (data + nextPageToken).
func (c *Client) LoadObjectSet(ctx context.Context, ontology string, body json.RawMessage) (json.RawMessage, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/objectSets/load"
	return c.PostJSONRaw(ctx, path, body)
}

// CreateTemporaryObjectSetRaw POSTs an ObjectSet definition (as raw JSON)
// and returns the {objectSetRid, expiresAt?} envelope verbatim. The
// strongly-typed CreateTemporaryObjectSet above remains the preferred API
// when the caller already has a Go map; this raw flavour exists so the
// `weave objectset create-temporary` CLI can forward user-authored JSON
// without round-tripping through map[string]any.
func (c *Client) CreateTemporaryObjectSetRaw(ctx context.Context, ontology string, body json.RawMessage) (json.RawMessage, error) {
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/objectSets/createTemporary"
	return c.PostJSONRaw(ctx, path, body)
}
