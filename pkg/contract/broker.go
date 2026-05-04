package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BrokerClient talks to a Pact Broker (https://docs.pact.io/pact_broker)
// HTTP API. Only the two endpoints the Weave US-445 flow needs are
// implemented — PUT a pact for one (provider, consumer, version) triplet
// and GET the latest pacts for a provider — so this stays a tiny, CGO-free
// dependency rather than pulling in pact-go (which requires the Pact native
// library binary on every CI runner).
type BrokerClient struct {
	baseURL string
	options BrokerClientOptions
	http    *http.Client
}

// BrokerClientOptions tunes a BrokerClient. AuthHeader, when non-empty, is
// stamped onto every outbound request — accepts whatever the deployed
// pact-broker is configured to accept (`Bearer <token>` for the OSS
// pact-broker behind a reverse proxy; `Basic ...` for the bundled basic-auth
// mode; empty for an open broker on a private network).
type BrokerClientOptions struct {
	AuthHeader string
	HTTPClient *http.Client
	Timeout    time.Duration
}

// NewBrokerClient builds a BrokerClient pointing at baseURL (e.g. the env
// var WEAVE_PACT_BROKER_URL). Trailing slashes on baseURL are normalised so
// callers can pass either `https://broker:9292` or `https://broker:9292/`.
func NewBrokerClient(baseURL string, opts BrokerClientOptions) *BrokerClient {
	httpClient := opts.HTTPClient
	if httpClient == nil {
		timeout := opts.Timeout
		if timeout == 0 {
			timeout = 30 * time.Second
		}
		httpClient = &http.Client{Timeout: timeout}
	}
	return &BrokerClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		options: opts,
		http:    httpClient,
	}
}

// BrokerError carries the broker's HTTP error response. Surfaced as a typed
// error so callers can `errors.As` to retry-or-fail on specific status codes
// (e.g. 409 conflict on duplicate version) without parsing strings.
type BrokerError struct {
	Status int
	Body   string
	URL    string
}

func (e *BrokerError) Error() string {
	return fmt.Sprintf("pact broker %s returned %d: %s", e.URL, e.Status, e.Body)
}

// PublishPact PUTs the pact JSON to the canonical Pact Broker endpoint:
//
//	PUT /pacts/provider/{provider}/consumer/{consumer}/version/{version}
//
// version is the consumer version string (typically a semver or git SHA);
// the broker uses this to dedupe re-publishes and drive matrix queries.
func (c *BrokerClient) PublishPact(p *Pact, version string) error {
	if p == nil {
		return errors.New("contract: pact is required")
	}
	if strings.TrimSpace(p.Consumer.Name) == "" {
		return errors.New("contract: pact.consumer.name is required for publish")
	}
	if strings.TrimSpace(p.Provider.Name) == "" {
		return errors.New("contract: pact.provider.name is required for publish")
	}
	if strings.TrimSpace(version) == "" {
		return errors.New("contract: pact version is required for publish")
	}
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("contract: marshal pact: %w", err)
	}
	target := fmt.Sprintf("%s/pacts/provider/%s/consumer/%s/version/%s",
		c.baseURL,
		url.PathEscape(p.Provider.Name),
		url.PathEscape(p.Consumer.Name),
		url.PathEscape(version),
	)
	req, err := http.NewRequest(http.MethodPut, target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("contract: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/hal+json, application/json")
	if c.options.AuthHeader != "" {
		req.Header.Set("Authorization", c.options.AuthHeader)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("contract: PUT %s: %w", target, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return &BrokerError{Status: resp.StatusCode, Body: string(respBody), URL: target}
	}
	return nil
}

// brokerLatestEnvelope is the HAL response shape the broker emits for
// /pacts/provider/{p}/latest — each entry under `_links.pacts[]` carries
// a direct `href` link to the pact JSON.
type brokerLatestEnvelope struct {
	Links struct {
		Pacts []struct {
			Href string `json:"href"`
		} `json:"pacts"`
	} `json:"_links"`
}

// FetchProviderPacts pulls every consumer's latest pact for the given
// provider. Used by the verifier side to discover what to replay against
// the running server, mirroring `pact-broker can-i-deploy` semantics
// without binding to the Ruby CLI.
func (c *BrokerClient) FetchProviderPacts(provider string) ([]*Pact, error) {
	if strings.TrimSpace(provider) == "" {
		return nil, errors.New("contract: provider is required")
	}
	indexURL := fmt.Sprintf("%s/pacts/provider/%s/latest",
		c.baseURL, url.PathEscape(provider))
	envelope, err := c.fetchJSON(indexURL)
	if err != nil {
		return nil, err
	}
	var index brokerLatestEnvelope
	if err := json.Unmarshal(envelope, &index); err != nil {
		return nil, fmt.Errorf("contract: parse broker index: %w", err)
	}
	pacts := make([]*Pact, 0, len(index.Links.Pacts))
	for _, ref := range index.Links.Pacts {
		if ref.Href == "" {
			continue
		}
		body, err := c.fetchJSON(ref.Href)
		if err != nil {
			return nil, err
		}
		pact, err := LoadPactBytes(body)
		if err != nil {
			return nil, fmt.Errorf("contract: parse pact at %s: %w", ref.Href, err)
		}
		pacts = append(pacts, pact)
	}
	return pacts, nil
}

func (c *BrokerClient) fetchJSON(target string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("contract: build request: %w", err)
	}
	req.Header.Set("Accept", "application/hal+json, application/json")
	if c.options.AuthHeader != "" {
		req.Header.Set("Authorization", c.options.AuthHeader)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contract: GET %s: %w", target, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, &BrokerError{Status: resp.StatusCode, Body: string(body), URL: target}
	}
	return io.ReadAll(resp.Body)
}
