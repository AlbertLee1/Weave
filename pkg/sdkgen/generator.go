package sdkgen

import (
	"context"
	"fmt"
)

// GeneratedFile is a single file produced by a Generator.
type GeneratedFile struct {
	Path    string // relative path inside the output directory / zip
	Content []byte
}

// Generator produces SDK source files for a target language.
type Generator interface {
	// Language returns the target language identifier (e.g. "ts", "python", "go").
	Language() string

	// Generate produces the SDK source files from the given schema.
	Generate(ctx context.Context, schema OntologySchema) ([]GeneratedFile, error)
}

// GetGenerator returns the Generator for the given language, or an error
// if the language is not supported.
func GetGenerator(lang string) (Generator, error) {
	switch lang {
	case "ts":
		return &tsGenerator{}, nil
	case "python":
		return &pythonGenerator{}, nil
	case "go":
		return &goGenerator{}, nil
	default:
		return nil, fmt.Errorf("unsupported language: %q", lang)
	}
}
