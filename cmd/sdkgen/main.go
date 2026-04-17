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
	"time"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/sdkgen"
)

func main() {
	ontology := flag.String("ontology", "", "Ontology API name (required)")
	lang := flag.String("lang", "ts", "Target language: ts, python, go")
	output := flag.String("output", "sdk-output", "Output directory")
	serverURL := flag.String("server-url", "http://localhost:9117", "Weave server URL")
	diff := flag.Bool("diff", false, "Compare ontology at --server-url with the previously generated SDK in --output and print changes (does not write files)")
	flag.Parse()

	if *ontology == "" {
		fmt.Fprintln(os.Stderr, "error: --ontology is required")
		flag.Usage()
		os.Exit(1)
	}

	export, err := fetchOntologyExport(*serverURL, *ontology)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	schema := buildSchema(export)

	if *diff {
		if err := runDiff(*output, schema); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	gen, err := sdkgen.GetGenerator(*lang)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// If a previous SDK was generated in this output directory, feed its
	// schema snapshot to the generator so CHANGELOG.md reflects the real diff.
	if prev, ok := loadPreviousMetadata(*output); ok && prev.Schema != nil {
		schema.Previous = prev.Schema
	}
	schema.ServerURL = *serverURL
	schema.GeneratedAt = time.Now().UTC()

	files, err := gen.Generate(context.Background(), schema)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error generating SDK: %v\n", err)
		os.Exit(1)
	}

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

func fetchOntologyExport(serverURL, ontology string) (*oms.OntologyExport, error) {
	exportURL := fmt.Sprintf("%s/api/v2/ontologies/%s/export", serverURL, ontology)
	resp, err := http.Get(exportURL)
	if err != nil {
		return nil, fmt.Errorf("fetching ontology: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}
	var export oms.OntologyExport
	if err := json.NewDecoder(resp.Body).Decode(&export); err != nil {
		return nil, fmt.Errorf("decoding export: %w", err)
	}
	return &export, nil
}

func buildSchema(export *oms.OntologyExport) sdkgen.OntologySchema {
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

	return schema
}

// loadPreviousMetadata reads <dir>/.weave-sdk.json if it exists.
func loadPreviousMetadata(dir string) (sdkgen.SDKMetadata, bool) {
	path := filepath.Join(dir, sdkgen.MetadataFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		return sdkgen.SDKMetadata{}, false
	}
	var m sdkgen.SDKMetadata
	if err := json.Unmarshal(data, &m); err != nil {
		return sdkgen.SDKMetadata{}, false
	}
	return m, true
}

func runDiff(outputDir string, current sdkgen.OntologySchema) error {
	prev, ok := loadPreviousMetadata(outputDir)
	if !ok {
		fmt.Printf("No previous SDK metadata found at %s; treat current ontology (version %d) as initial.\n",
			filepath.Join(outputDir, sdkgen.MetadataFilename), current.Ontology.Version)
		return nil
	}

	var prevSchema *sdkgen.OntologySchema
	if prev.Schema != nil {
		prevSchema = prev.Schema
	}

	diff := sdkgen.DiffSchemas(prevSchema, current)
	fmt.Printf("Ontology: %s\n", current.Ontology.APIName)
	fmt.Printf("Previous version: %d (generated %s)\n", prev.OntologyVersion, prev.GeneratedAt.Format(time.RFC3339))
	fmt.Printf("Current version:  %d\n\n", current.Ontology.Version)

	if !diff.HasChanges() {
		fmt.Println("No schema changes.")
		return nil
	}

	if len(diff.AddedObjects) > 0 {
		fmt.Println("Added ObjectTypes:")
		for _, ot := range diff.AddedObjects {
			fmt.Printf("  + %s\n", ot.APIName)
		}
	}
	if len(diff.RemovedObjects) > 0 {
		fmt.Println("Removed ObjectTypes:")
		for _, ot := range diff.RemovedObjects {
			fmt.Printf("  - %s\n", ot.APIName)
		}
	}
	if len(diff.ModifiedObjects) > 0 {
		fmt.Println("Modified ObjectTypes:")
		for _, m := range diff.ModifiedObjects {
			fmt.Printf("  ~ %s\n", m.APIName)
			for _, p := range m.AddedProperties {
				fmt.Printf("      + property %s\n", p)
			}
			for _, p := range m.RemovedProperties {
				fmt.Printf("      - property %s\n", p)
			}
			for _, p := range m.ModifiedProperties {
				fmt.Printf("      ~ property %s\n", p)
			}
		}
	}
	if len(diff.AddedLinks) > 0 {
		fmt.Println("Added LinkTypes:")
		for _, lt := range diff.AddedLinks {
			fmt.Printf("  + %s (%s → %s)\n", lt.APIName, lt.SourceObjectType, lt.TargetObjectType)
		}
	}
	if len(diff.RemovedLinks) > 0 {
		fmt.Println("Removed LinkTypes:")
		for _, lt := range diff.RemovedLinks {
			fmt.Printf("  - %s\n", lt.APIName)
		}
	}
	if len(diff.AddedActions) > 0 {
		fmt.Println("Added ActionTypes:")
		for _, at := range diff.AddedActions {
			fmt.Printf("  + %s\n", at.APIName)
		}
	}
	if len(diff.RemovedActions) > 0 {
		fmt.Println("Removed ActionTypes:")
		for _, at := range diff.RemovedActions {
			fmt.Printf("  - %s\n", at.APIName)
		}
	}
	if len(diff.ModifiedActions) > 0 {
		fmt.Println("Modified ActionTypes:")
		for _, at := range diff.ModifiedActions {
			fmt.Printf("  ~ %s\n", at.APIName)
		}
	}
	return nil
}
