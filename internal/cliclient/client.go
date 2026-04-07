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

// ApplyActionResponse mirrors the apply endpoint.
type ApplyActionResponse struct {
	ActionRID string           `json:"actionRid"`
	Edits     []map[string]any `json:"edits"`
	BatchID   string           `json:"batchId,omitempty"`
	Offset    int64            `json:"offset,omitempty"`
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

// SearchObjects POSTs a where-clause search request.
func (c *Client) SearchObjects(ctx context.Context, ontology, objectType string, where map[string]any) (*ObjectPage, error) {
	body := map[string]any{"where": where}
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
func (c *Client) ApplyAction(ctx context.Context, ontology, actionType string, parameters map[string]any) (*ApplyActionResponse, error) {
	body := map[string]any{
		"actionType": actionType,
		"parameters": parameters,
	}
	path := "/api/v2/ontologies/" + url.PathEscape(ontology) + "/actions/apply"
	var resp ApplyActionResponse
	if err := c.do(ctx, http.MethodPost, path, body, &resp); err != nil {
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
