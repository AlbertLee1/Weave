package schema

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
)

// MaxInferenceRequestBytes caps the inline-sample upload size at 8 MiB.
// The handler only stores the parsed schema, never the raw bytes; the
// cap exists to keep memory bounded for "what if the user pastes a
// 1 GiB CSV" pathological inputs.
const MaxInferenceRequestBytes = 8 << 20

// InferRequest is the inline-payload body the inference endpoint
// accepts. The Source field is reserved for future "fetch from URL /
// objectset / bucket" wiring (US-292+) — when empty we expect Sample.
type InferRequest struct {
	Format  string         `json:"format"`
	Sample  string         `json:"sample"`
	Options InferOptionsIn `json:"options,omitempty"`
}

// InferOptionsIn mirrors Options on the wire. We expose Delimiter as a
// string for ergonomic JSON ("\t" rather than "9"); the handler maps
// to the rune the inference engine expects.
type InferOptionsIn struct {
	SampleRows int    `json:"sampleRows,omitempty"`
	HasHeader  *bool  `json:"hasHeader,omitempty"`
	Delimiter  string `json:"delimiter,omitempty"`
}

// Handler exposes POST /api/v2/pipelines/schema/infer.
type Handler struct{}

// NewHandler returns a new inference handler. The handler is
// intentionally stateless — sample bytes flow through the request and
// nothing is persisted.
func NewHandler() *Handler { return &Handler{} }

// RegisterRoutes mounts the inference route on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/v2/pipelines/schema/infer", h.InferSchema)
}

// InferSchema POST /api/v2/pipelines/schema/infer.
//
// Request body:
//
//	{
//	  "format": "csv" | "json" | "ndjson",
//	  "sample": "<inline content>",
//	  "options": {
//	    "sampleRows": 1000,
//	    "hasHeader": true,
//	    "delimiter": ","
//	  }
//	}
//
// Response: schema.Result.
func (h *Handler) InferSchema(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxInferenceRequestBytes)
	defer r.Body.Close()

	var req InferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("SampleTooLarge", map[string]string{
				"reason": "request body exceeds inference cap",
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	format := Format(strings.ToLower(strings.TrimSpace(req.Format)))
	if format == "" {
		format = FormatCSV
	}
	if err := validateFormat(format); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("UnsupportedFormat", map[string]string{
			"format": req.Format,
			"reason": err.Error(),
		}))
		return
	}
	if req.Sample == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingSample", map[string]string{
			"reason": "sample content must not be empty",
		}))
		return
	}
	opts := Options{
		SampleRows: req.Options.SampleRows,
		HasHeader:  req.Options.HasHeader == nil || *req.Options.HasHeader,
	}
	if d := strings.TrimSpace(req.Options.Delimiter); d != "" {
		runes := []rune(d)
		if len(runes) != 1 {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidDelimiter", map[string]string{
				"reason":    "delimiter must be a single character",
				"delimiter": req.Options.Delimiter,
			}))
			return
		}
		opts.Delimiter = runes[0]
	}

	res, err := infer(format, strings.NewReader(req.Sample), opts)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InferenceFailed", map[string]string{
			"reason": err.Error(),
			"format": string(format),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, res)
}

// infer dispatches to the format-specific reader.
func infer(format Format, r io.Reader, opts Options) (*Result, error) {
	switch format {
	case FormatCSV:
		return InferCSV(r, opts)
	case FormatJSON:
		return InferJSON(r, opts)
	case FormatNDJSON:
		return InferNDJSON(r, opts)
	default:
		return nil, errors.New("unsupported format")
	}
}
