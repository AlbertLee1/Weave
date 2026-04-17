package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/sdkgen"
)

func main() {
	ontology := flag.String("ontology", "", "Ontology API name (required)")
	lang := flag.String("lang", "ts", "Target language: ts, python, go")
	output := flag.String("output", "sdk-output", "Output directory")
	serverURL := flag.String("server-url", "http://localhost:9117", "Weave server URL")
	flag.Parse()

	if *ontology == "" {
		fmt.Fprintln(os.Stderr, "error: --ontology is required")
		flag.Usage()
		os.Exit(1)
	}

	gen, err := sdkgen.GetGenerator(*lang)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Fetch ontology export from server
	exportURL := fmt.Sprintf("%s/api/v2/ontologies/%s/export", *serverURL, *ontology)
	resp, err := http.Get(exportURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error fetching ontology: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "error: server returned %d: %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	var export oms.OntologyExport
	if err := json.NewDecoder(resp.Body).Decode(&export); err != nil {
		fmt.Fprintf(os.Stderr, "error decoding export: %v\n", err)
		os.Exit(1)
	}

	// Convert to schema
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

	// Generate files
	files, err := gen.Generate(context.Background(), schema)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error generating SDK: %v\n", err)
		os.Exit(1)
	}

	// Write files to output directory
	for _, f := range files {
		outPath := filepath.Join(*output, f.Path)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "error creating directory: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(outPath, f.Content, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing %s: %v\n", f.Path, err)
			os.Exit(1)
		}
		fmt.Printf("  wrote %s\n", outPath)
	}

	fmt.Printf("\nSDK generated in %s/ (%s, %d files)\n", *output, gen.Language(), len(files))
}
