// Weave Go quickstart — a 5 minute hello-world.
//
// Talks to a local Weave server over its REST API using net/http (stdlib
// only — no SDK package required). Once you've gotten a feel for the API,
// generate a fully-typed SDK with `weave-cli sdk gen --lang go --ontology
// <api-name>` for richer ergonomics.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

type ontology struct {
	APIName     string `json:"apiName"`
	DisplayName string `json:"displayName"`
}

type objectType struct {
	APIName     string `json:"apiName"`
	DisplayName string `json:"displayName"`
}

type listResponse[T any] struct {
	Data          []T    `json:"data"`
	NextPageToken string `json:"nextPageToken,omitempty"`
}

type objectPage struct {
	Data          []map[string]any `json:"data"`
	NextPageToken string           `json:"nextPageToken,omitempty"`
}

type client struct {
	baseURL string
	token   string
	http    *http.Client
}

func newClient() *client {
	base := os.Getenv("WEAVE_BASE_URL")
	if base == "" {
		base = "http://localhost:9117"
	}
	return &client{
		baseURL: base,
		token:   os.Getenv("WEAVE_TOKEN"),
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *client) get(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("weave %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *client) listOntologies() ([]ontology, error) {
	var resp listResponse[ontology]
	if err := c.get("/api/v2/ontologies", &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *client) listObjectTypes(ontologyName string) ([]objectType, error) {
	var resp listResponse[objectType]
	path := "/api/v2/ontologies/" + url.PathEscape(ontologyName) + "/objectTypes"
	if err := c.get(path, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *client) listObjects(ontologyName, otAPIName string, pageSize int) (*objectPage, error) {
	var page objectPage
	path := fmt.Sprintf(
		"/api/v2/ontologies/%s/objects/%s?pageSize=%d",
		url.PathEscape(ontologyName), url.PathEscape(otAPIName), pageSize,
	)
	if err := c.get(path, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func run() error {
	c := newClient()

	fmt.Println("=== Ontologies ===")
	ontologies, err := c.listOntologies()
	if err != nil {
		return err
	}
	for _, o := range ontologies {
		fmt.Printf("- %s\t%s\n", o.APIName, o.DisplayName)
	}
	if len(ontologies) == 0 {
		fmt.Println("(no ontologies — load a fixture e.g. testdata/northwind to see more)")
		return nil
	}

	ontologyName := ontologies[0].APIName
	fmt.Printf("=== Object types in %s ===\n", ontologyName)
	types, err := c.listObjectTypes(ontologyName)
	if err != nil {
		return err
	}
	for _, t := range types {
		fmt.Printf("- %s\t%s\n", t.APIName, t.DisplayName)
	}
	if len(types) == 0 {
		return nil
	}

	otName := types[0].APIName
	fmt.Printf("=== First 5 %s ===\n", otName)
	page, err := c.listObjects(ontologyName, otName, 5)
	if err != nil {
		return err
	}
	for _, row := range page.Data {
		pk := row["__primaryKey"]
		raw, _ := json.Marshal(row)
		fmt.Printf("- %v\t%s\n", pk, string(raw))
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
