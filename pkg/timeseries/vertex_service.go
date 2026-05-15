package timeseries

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Agg names the supported window aggregation functions for VertexService.
type Agg string

const (
	AggAvg  Agg = "AVG"
	AggMin  Agg = "MIN"
	AggMax  Agg = "MAX"
	AggSum  Agg = "SUM"
	AggLast Agg = "LAST"
)

// BucketedPoint is one row in a Query result. Time is the left edge of the
// time_bucket window; Value is the aggregate over the window.
type BucketedPoint struct {
	Time  time.Time `json:"ts"`
	Value float64   `json:"value"`
}

// VertexQueryResult is the response surface for VertexService.Query.
//
// Warning is "missing_data" when no data exists within the configured
// missingDataWarningHours of the requested To timestamp; LastObservedAt
// then carries the most recent observation we *do* have on record (or nil
// when the series is empty).
type VertexQueryResult struct {
	Points         []BucketedPoint `json:"points"`
	Warning        string          `json:"warning,omitempty"`
	LastObservedAt *time.Time      `json:"lastObservedAt,omitempty"`
}

// VertexQuery is the Query parameter bundle.
type VertexQuery struct {
	ObjectRID  string
	Property   string
	From       time.Time
	To         time.Time
	Agg        Agg
	Bucket     time.Duration
	ScenarioID string
}

// ScenarioOverlayReader supplies the scenario layer of the time series read
// path. VertexService.Query calls GetWindowedScalarOverride exactly once per
// request; a non-nil return is treated as a scalar override that replaces
// every bucket value in the response.
//
// Returning ErrScenarioNotFound makes Query fail with a wrapping error so
// callers can map it to a 404. Returning (nil, nil) means "no override —
// proceed with the raw bucket values".
type ScenarioOverlayReader interface {
	GetWindowedScalarOverride(ctx context.Context, scenarioID, objectRID, property string) (*float64, error)
}

// ErrScenarioNotFound is the sentinel ScenarioOverlayReader implementations
// return when the supplied scenarioID has no corresponding scenario record.
var ErrScenarioNotFound = errors.New("timeseries: scenario not found")

// VertexService answers Foundry-Vertex window-aggregation queries against
// the VTX-028 object_time_series hypertable.
type VertexService struct {
	pool                    *pgxpool.Pool
	overlay                 ScenarioOverlayReader
	missingDataWarningHours int
}

// VertexOption configures a VertexService at construction time.
type VertexOption func(*VertexService)

// WithScenarioOverlay attaches a scenario overlay reader; without it Query
// silently treats every request as having no overlay even when ScenarioID
// is set.
func WithScenarioOverlay(r ScenarioOverlayReader) VertexOption {
	return func(s *VertexService) { s.overlay = r }
}

// WithMissingDataWarningHours sets the threshold for the missing_data
// warning. Default is 24h; pass any positive value to override. A value of
// 0 disables the warning entirely.
func WithMissingDataWarningHours(h int) VertexOption {
	return func(s *VertexService) { s.missingDataWarningHours = h }
}

// NewVertexService constructs a VertexService bound to a pgx pool that
// must point at a database with the VTX-028 migration applied.
func NewVertexService(pool *pgxpool.Pool, opts ...VertexOption) *VertexService {
	s := &VertexService{pool: pool, missingDataWarningHours: 24}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// aggSQL maps the public Agg enum to the inline SQL aggregate expression.
// LAST routes to TimescaleDB's `last(value, ts)` hyperfunction.
func aggSQL(a Agg) (string, error) {
	switch a {
	case AggAvg:
		return "AVG(value)", nil
	case AggMin:
		return "MIN(value)", nil
	case AggMax:
		return "MAX(value)", nil
	case AggSum:
		return "SUM(value)", nil
	case AggLast:
		return "last(value, ts)", nil
	default:
		return "", fmt.Errorf("timeseries: unsupported agg %q", a)
	}
}

// pgInterval renders a Go time.Duration as a Postgres INTERVAL literal
// (microsecond precision). pgx will bind it to the $1::INTERVAL cast.
func pgInterval(d time.Duration) string {
	return fmt.Sprintf("%d microseconds", d.Microseconds())
}
