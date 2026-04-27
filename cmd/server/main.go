package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liyang/weave/internal/config"
	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/aip"
	aiplogic "github.com/liyang/weave/pkg/aip/logic"
	"github.com/liyang/weave/pkg/attachment"
	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/cellsec"
	"github.com/liyang/weave/pkg/cipher"
	"github.com/liyang/weave/pkg/compliance"
	"github.com/liyang/weave/pkg/developer"
	"github.com/liyang/weave/pkg/featureflags"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/gdpr"
	"github.com/liyang/weave/pkg/geotemporal"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/masking"
	"github.com/liyang/weave/pkg/mcp"
	"github.com/liyang/weave/pkg/media"
	"github.com/liyang/weave/pkg/metrics"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/oss/objectset"
	"github.com/liyang/weave/pkg/rls"
	"github.com/liyang/weave/pkg/security"
	"github.com/liyang/weave/pkg/security/pii"
	"github.com/liyang/weave/pkg/sqlqueries"
	"github.com/liyang/weave/pkg/subscriptions"
	"github.com/liyang/weave/pkg/tenants"
	"github.com/liyang/weave/pkg/timeseries"
	"github.com/liyang/weave/pkg/tracing"
	"github.com/liyang/weave/pkg/transactions"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ServerDeps holds all server dependencies.
type ServerDeps struct {
	OmsRepo        oms.Repository
	UserRepo       auth.UserRepository
	APIKeyRepo     auth.APIKeyRepository
	RoleResolver   *auth.RoleResolver
	JWTSigner      *auth.JWTSigner
	RefreshService *auth.RefreshService
	IndexMgr       *index.Manager
	LinkResolver   links.LinkResolver
	OssSvc         oss.Service
	AggEngine      *aggregation.Engine
	ActionExecutor *actions.Executor
	ObjSetStore    *objectset.Store
	ObjSetExecutor *objectset.Executor
	// PolicyEngine is the shared row-level security engine (US-046). The
	// OSS service's Load/Search paths and the ObjectSet executor's
	// base/filter paths both read through it so a single SetPolicies call
	// updates every read surface.
	PolicyEngine    *security.Engine
	FunnelPublisher oss.IngestPublisher // US-061: may be *funnel.Publisher or stub
	FunnelConsumer  *funnel.Consumer
	// FunnelBroadcast is the in-process fan-out hub the SSE subscribe
	// endpoint (US-055) reads from. The consumer can opt in to publishing
	// events onto the hub via its OnChange hook; tests (and degraded-mode
	// bootstraps) may leave it nil and the SSE route will return a clean
	// 500 SSESubscribeNotConfigured.
	FunnelBroadcast *funnel.Broadcast
	AttachmentStore attachment.BlobStore
	// US-204: Media upload/download/delete API. Both fields must be wired
	// for the /api/v2/media routes to mount; in degraded mode (no PG) the
	// catalog is nil and the routes are not registered.
	MediaStore   *media.Store
	MediaCatalog oms.MediaAssetStore
	// US-210: Link Properties. LinkPropertyStore holds the edge-property
	// schema (new link_properties table); LinkEdgeStore is the narrow CRUD
	// surface over link_edges used by the PUT edges/properties endpoint and
	// the searchAround enrichment path. Both are served by the uncached
	// *PGRepository in the non-degraded bootstrap.
	LinkPropertyStore oms.LinkPropertyStore
	LinkEdgeStore     oms.LinkEdgeStore
	// US-214 Interface Method Signatures. InterfaceMethodStore holds the
	// interface_methods table CRUD; InterfaceMethodDispatcher (optional)
	// forwards a polymorphic invoke to the actions.Executor so the invoke
	// endpoint can actually run the resolved ActionType.
	InterfaceMethodStore      oms.InterfaceMethodStore
	InterfaceMethodDispatcher oms.InterfaceMethodActionDispatcher
	// US-223 Time-Travel Queries. HistorySnapshotStore is the uncached
	// *PGRepository view onto SnapshotObjectsAt. When wired alongside
	// OmsRepo the loadObjects handler honours `?asOf=<RFC3339>`; absent
	// the route falls through to TimeTravelUnavailable 400.
	HistorySnapshotStore historySnapshotStore
	// US-224 ObjectSet Versioned Snapshots. ObjectSetSnapshotStore is the
	// uncached *PGRepository view onto Create/GetObjectSetSnapshot. When
	// wired the POST /objectSets/{rid}/snapshot and GET
	// /objectSets/snapshots/{rid} routes are functional; absent the
	// snapshot endpoints return SnapshotsUnavailable 400 so the routes are
	// still mounted (and discoverable via OpenAPI / contract tests).
	ObjectSetSnapshotStore oms.ObjectSetSnapshotStore
	TimeSeriesStore        timeseries.Store
	GeotemporalStore       geotemporal.Store
	CipherDecryptor        cipher.Decryptor
	TransactionStore       transactions.Store
	SqlQueryEngine         sqlqueries.Engine
	IndexDocSource         index.LatestDocumentSource // Authoritative source for index.Rebuild (nil in degraded mode)
	AuditStore             audit.Store                // US-067: audit event store (nil = endpoint returns 503)
	IngestRateLimiter      oss.IngestRateLimiter      // US-063: per-ontology token-bucket (nil = no limit)
	WebSocketHub           *subscriptions.Hub         // US-132: WebSocket subscription hub (nil = endpoint not mounted)
	// US-141: Developer Console application registry. When nil the
	// /api/v2/developer/applications routes are not registered.
	ApplicationRepo developer.ApplicationRepository
	// US-144: Per-application API usage sample store (in-memory). Populated
	// by metrics.UsageMiddleware on every authenticated request and read by
	// GET /api/v2/developer/applications/{id}/usage. When nil the
	// middleware is still registered (Prometheus counters are always
	// emitted) but the /usage endpoint returns empty windows.
	UsageSamples *metrics.UsageSampleStore
	// US-142: Developer Console OAuth 2.0 authorization_code + PKCE flow.
	// When both AuthCodeRepo and OAuthTokenRepo are non-nil the /oauth/*
	// endpoints and OAuth bearer token validation are wired; nil leaves
	// them off and the auth middleware degrades to JWT / API-key only.
	AuthCodeRepo   developer.AuthorizationCodeRepository
	OAuthTokenRepo developer.OAuthTokenRepository
	// US-246: OIDC Authorization Code front-door. When Handler is non-nil
	// the /api/auth/oidc/login and /api/auth/oidc/callback endpoints are
	// registered; any misconfig (unreachable issuer, missing client ID)
	// leaves Handler nil and the routes are not mounted.
	OIDCHandler *auth.OIDCHandler
	// US-255: OIDC back-channel logout. Reuses the OIDC discovery /
	// verifier wired alongside the login handler; nil when the OIDC
	// front-door isn't mounted.
	OIDCLogoutHandler *auth.OIDCBackChannelLogoutHandler
	// US-248: SAML 2.0 SSO front-door. When non-nil the
	// /api/auth/saml/{metadata,login,acs} endpoints are registered;
	// any misconfig (missing IdP cert, unparseable PEM) leaves Handler
	// nil and the routes are not mounted.
	SAMLHandler *auth.SAMLHandler
	// US-255: SAML Single Logout (SLO). Reuses the gosaml2 SP wired
	// alongside the SAML login handler; nil when the SAML front-door
	// isn't mounted.
	SAMLSLOHandler *auth.SAMLSLOHandler
	// US-249: Service account admin CRUD. Populated from the uncached
	// *PGRepository-style wrapper in the PG bootstrap block; nil in
	// degraded mode so the /api/admin/service-accounts routes are not
	// mounted.
	ServiceAccountRepo auth.ServiceAccountRepository
	// US-251: Groups / Roles admin CRUD. Populated from the uncached
	// *PGRepository wrappers in the PG bootstrap block; nil in degraded
	// mode so the /api/admin/{groups,roles,users}/... routes are not
	// mounted.
	GroupRepo auth.GroupRepository
	RoleRepo  auth.RoleRepository
	// US-259: Marking grant admin CRUD. MarkingRepo is the canonical
	// request-hot-path surface (also shared with login / OIDC / SAML JWT
	// enrichment); MarkingAdminRepo is the admin-side extension that
	// powers /api/admin/markings list-grants endpoints. Both are
	// populated from the uncached *PGMarkingRepository in the PG
	// bootstrap block; nil in degraded mode so the routes are not
	// mounted.
	MarkingRepo      auth.MarkingRepository
	MarkingAdminRepo auth.MarkingGrantAdminRepository
	// US-256 Row-Level Security. RowPolicyStore is the admin-CRUD surface
	// over the row_policies table; RowPolicyEngine compiles applicable
	// predicates into Bleve queries at read time. Both are populated from
	// the PG bootstrap block and left nil in degraded mode — the routes
	// are not mounted and the OSS service's read paths observe no
	// additional filter.
	RowPolicyStore  rls.Store
	RowPolicyEngine *rls.Engine
	// US-257 Column-Level Masking. ColumnMaskStore is the admin-CRUD
	// surface over the column_masks table; ColumnMaskEngine compiles the
	// applicable mask transforms (hash/redact/partial) for a caller at
	// read time. Both are populated from the PG bootstrap block and left
	// nil in degraded mode — the routes are not mounted and the OSS
	// service's read paths emit property values unchanged.
	ColumnMaskStore  masking.Store
	ColumnMaskEngine *masking.Engine
	// US-258 Cell-Level Security. CellMaskStore is the admin-CRUD surface
	// over the cell_masks table; CellMaskEngine compiles the applicable
	// per-(ObjectType, primaryKey) mask transforms at read time. Both are
	// populated from the PG bootstrap block and left nil in degraded mode
	// — the routes are not mounted and the OSS service's read paths skip
	// cell-level rewriting.
	CellMaskStore  cellsec.Store
	CellMaskEngine *cellsec.Engine
	// US-253: TOTP-based MFA. MFAStore is the narrow persistence surface
	// over the new users.mfa_secret / users.mfa_enabled columns; satisfied
	// by the uncached *PGUserRepository. MFAChallenges bridges the login
	// handler (which mints a challenge after password verification when the
	// user has MFA enabled) and /api/auth/mfa/verify (which consumes it).
	// Both must be wired for /api/auth/mfa/* routes to mount; in degraded
	// mode the routes are skipped and the login handler emits tokens
	// directly.
	MFAStore      auth.MFASecretStore
	MFAChallenges *auth.MFAChallengeStore
	// US-254: active-session inventory for GET/DELETE /api/auth/sessions.
	// Populated by the PG bootstrap block; nil in degraded mode so the
	// routes are not mounted and login/refresh skip the session insert.
	SessionStore auth.SessionStore
	// US-267: GDPR right-to-be-forgotten async erase. GDPRJobStore tracks
	// per-job status; GDPRRedactions backs the audit RedactingStore
	// decorator. Both populate from the PG bootstrap block; nil in
	// degraded mode so the /api/admin/gdpr/* routes are not mounted and
	// the audit store skips the redaction overlay.
	GDPRJobStore   gdpr.JobStore
	GDPRRedactions audit.RedactionStore
	// US-268: GDPR data-portability export. Optional — when nil the
	// /api/admin/gdpr/export route emits GDPRExportUnavailable. Wired in
	// the PG bootstrap block once the user repo + media catalog + audit
	// store are available.
	GDPRExporter *gdpr.Exporter
	// US-270: Compliance control-evidence report generator. Composes
	// over AuditStore + MarkingRepo + OmsRepo + the three security-
	// surface stores; nil means no source is wired and the
	// /api/admin/compliance/report route is not mounted.
	ComplianceGenerator *compliance.Generator
	// US-276: Feature Flags. FeatureFlagStore is the CRUD surface for the
	// /api/admin/feature-flags/* admin endpoints; FeatureFlagManager is
	// the read-side facade handlers call via featureflags.HasFlag. Both
	// populate from the PG bootstrap block; nil in degraded mode so the
	// admin routes are not mounted and in-process HasFlag checks return
	// false (fail-closed).
	FeatureFlagStore   featureflags.Store
	FeatureFlagManager *featureflags.Manager
	// US-277: Multi-tenant quotas. TenantQuotaStore is the CRUD surface
	// for /api/admin/tenant-quotas/* admin endpoints; TenantQuotaManager
	// is the read-side facade middleware uses to gate per-tenant QPS.
	// Both populate from the PG bootstrap block; nil in degraded mode so
	// the admin routes are not mounted and middleware passes everything
	// through (no per-tenant QPS cap).
	TenantQuotaStore   tenants.Store
	TenantQuotaManager *tenants.Manager
	// US-279: AIP Threads. AIPStore persists threads + messages; the
	// AIPRegistry resolves the named provider (mock/openai/anthropic).
	// nil-AIPStore degraded mode leaves /api/v2/aip/threads/* routes
	// unmounted; nil-AIPRegistry leaves SendMessage returning
	// AIPProviderNotConfigured. The mock provider is always registered
	// when AIPRegistry is wired.
	AIPStore    aip.Store
	AIPRegistry *aip.Registry
	// AIPTools is the ToolRegistry the SendMessage handler resolves
	// model-requested function calls through (US-284). nil disables
	// function-calling and the SendMessage loop runs at most one
	// Provider.Complete cycle (legacy single-turn behaviour).
	AIPTools *aip.ToolRegistry
	// AIPToolCatalog persists custom tool definitions (US-285) the LLM
	// may invoke alongside the built-in tools. The PG-backed catalog
	// is loaded into AIPTools at boot and the admin /api/v2/aip/tools
	// CRUD endpoints keep the in-process registry in sync. nil leaves
	// the admin endpoints unmounted and the registry confined to
	// whatever the caller pre-populated.
	AIPToolCatalog aip.ToolCatalog
	// AIPToolInvoker is the FunctionInvoker that custom tool entries
	// dispatch through. nil makes catalog-backed tools surface a
	// clean ErrToolHandlerNotConfigured at execute-time so operators
	// notice the missing FunctionExecutor wiring.
	AIPToolInvoker aip.FunctionInvoker
	// US-281: AIP Logic Flow store + executor. The store persists
	// flow definitions and run rows; the executor walks the DAG via the
	// AIPRegistry (LLM nodes) + AIPLogicTools (tool nodes). Both nil
	// in degraded mode so /api/v2/aip/logic-flows/* routes are
	// unmounted.
	AIPLogicStore aiplogic.Store
	AIPLogicTools aiplogic.ToolRegistry
	CORSOrigins        []string // Allowed CORS origins (empty = disabled)
	// Raw handles stashed for health probes. May be nil in degraded mode.
	PGPool   *pgxpool.Pool
	NATSConn *nats.Conn
}

// ProbePG satisfies HealthProbes. Returns ErrProbeUnconfigured when no PG
// pool is wired (degraded mode), nil when the pool pings successfully, or
// the underlying ping error.
func (d *ServerDeps) ProbePG(ctx context.Context) error {
	if d == nil || d.PGPool == nil {
		return ErrProbeUnconfigured
	}
	return d.PGPool.Ping(ctx)
}

// ProbeNATS satisfies HealthProbes. Returns ErrProbeUnconfigured when no
// NATS connection is wired, nil when the connection is CONNECTED, or an
// error describing the current status.
func (d *ServerDeps) ProbeNATS() error {
	if d == nil || d.NATSConn == nil {
		return ErrProbeUnconfigured
	}
	if d.NATSConn.Status() != nats.CONNECTED {
		return fmt.Errorf("nats not connected: status=%s", d.NATSConn.Status())
	}
	return nil
}

// ProbeBleve satisfies HealthProbes. Returns ErrProbeUnconfigured when no
// index manager is wired, nil otherwise. (NumIndexes is cheap and doesn't
// error on a healthy Manager, so reaching it means the component is up.)
func (d *ServerDeps) ProbeBleve() error {
	if d == nil || d.IndexMgr == nil {
		return ErrProbeUnconfigured
	}
	return nil
}

func NewRouter() *chi.Mux {
	r := chi.NewRouter()

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	return r
}

// NewFullRouter creates a fully-wired chi router with all API routes.
func NewFullRouter(deps *ServerDeps) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware (applied to all routes including static assets)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(SecurityHeadersMiddleware())
	// US-271: open one OpenTelemetry server-kind span per request so every
	// handler is wrapped automatically. Stays cheap when tracing is
	// disabled — the global TracerProvider is the SDK no-op until
	// pkg/tracing.Init swaps it. Runs AFTER middleware.RequestID so the
	// chi-issued request id is already in context.
	r.Use(tracing.HTTPMiddleware())
	// US-271: enrich every span + request context with W3C Baggage members
	// for request_id and (when authenticated) the caller's user_id. Pulls
	// the user via auth.UserFromContext through a function literal so
	// pkg/tracing stays free of a pkg/auth import.
	r.Use(tracing.BaggageMiddleware(func(ctx context.Context) string {
		if u := auth.UserFromContext(ctx); u != nil {
			return u.ID
		}
		return ""
	}))
	// US-264: stamp caller IP + User-Agent onto every request context so the
	// OSS data-access auditor (pkg/oss) can record audit_events rows without
	// taking an *http.Request dependency. Runs AFTER middleware.RealIP so
	// X-Forwarded-For rewriting has already taken effect.
	r.Use(audit.ClientInfoMiddleware)
	if deps.CORSOrigins != nil && len(deps.CORSOrigins) > 0 {
		r.Use(CORSMiddleware(deps.CORSOrigins))
	}
	// US-069: per-endpoint rate limiting with default fallback.
	rateLimitRules, defaultRateLimitRule := DefaultRateLimitRules()
	r.Use(NewRateLimitMiddlewareWithDefault(rateLimitRules, defaultRateLimitRule))
	// US-276: stamp the feature-flag manager on every request context so
	// downstream handlers can call featureflags.HasFlag(ctx, name, user)
	// without threading the manager explicitly. Nil manager is a no-op —
	// degraded-mode deployments leave it unset so every check fails
	// closed.
	if deps.FeatureFlagManager != nil {
		mgr := deps.FeatureFlagManager
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(featureflags.WithManager(req.Context(), mgr)))
			})
		})
	}
	// US-277: per-tenant QPS gating. Reads auth.User.Attributes["realm"]
	// after the auth middleware runs and rejects with 429 once a tenant
	// exceeds its configured rate. Anonymous callers and tenants
	// without a quota row pass through. The middleware also stamps the
	// Manager on the request context so write handlers can call
	// CheckObjectQuota / CheckStorageQuota.
	if deps.TenantQuotaManager != nil {
		r.Use(tenants.Middleware(deps.TenantQuotaManager))
	}

	// Health endpoints (public, no auth required)
	// /health and /health/live are the k8s liveness probe: always return 200
	// {"status":"alive"}. /health/ready is the k8s readiness probe: checks
	// PG/NATS/Bleve, 503 if any configured dependency is unhealthy. The
	// /health/live alias matches the conventional kubernetes path so a
	// rolling-upgrade probeSpec can pin the explicit endpoint while legacy
	// /health stays available for compatibility.
	livenessHandler := LivenessHandler()
	r.Method(http.MethodGet, "/health", livenessHandler)
	r.Method(http.MethodGet, "/health/live", livenessHandler)
	r.Method(http.MethodGet, "/health/ready", ReadinessHandler(deps))

	// OpenAPI & Swagger UI (public)
	r.Method(http.MethodGet, "/api/openapi.yaml", openapiSpecHandler())
	r.Method(http.MethodGet, "/swagger/", swaggerUIHandler())
	r.Method(http.MethodGet, "/swagger", http.RedirectHandler("/swagger/", http.StatusMovedPermanently))

	// MCP server (public JSON-RPC 2.0 endpoint for AI agents)
	if deps.OssSvc != nil && deps.OmsRepo != nil {
		mcpSrv := mcp.NewServer(deps.OssSvc, deps.OmsRepo, deps.ActionExecutor)
		// US-046: wire the semantic searcher so weave_semantic_search and
		// weave_ask_objectset can run nearestNeighbors queries via the
		// ObjectSet executor. Optional — when nil the AI search tools
		// return a clear "not configured" error.
		if deps.ObjSetExecutor != nil {
			mcpSrv.SetSemanticSearcher(newExecutorSemanticSearcher(deps.ObjSetExecutor))
		}
		// US-286: expose temporary ObjectSet entries as MCP resources alongside
		// the ontology catalogue. Optional — when no Store is wired the
		// resources/list and resources/read methods still work for ontologies.
		if deps.ObjSetStore != nil {
			mcpSrv.SetObjectSetCatalog(newObjectSetCatalogAdapter(deps.ObjSetStore))
		}
		r.Method(http.MethodPost, "/mcp", mcp.NewHTTPHandler(mcpSrv))
	}

	// Prometheus metrics scrape endpoint (public).
	r.Method(http.MethodGet, "/metrics", promhttp.HandlerFor(metrics.Default(), promhttp.HandlerOpts{}))

	// Public auth endpoints — login/refresh/logout are NOT behind the
	// auth middleware because they are how clients obtain or rotate tokens.
	if deps.JWTSigner != nil && deps.RefreshService != nil && deps.UserRepo != nil {
		// US-082: construct a PGMarkingRepository so the login handler can
		// include the user's held markings in the JWT for marking-based row
		// filter enforcement.
		var markingRepo auth.MarkingRepository
		if deps.PGPool != nil {
			markingRepo = auth.NewPGMarkingRepository(deps.PGPool)
		}
		loginRateLimit := 5
		if v := os.Getenv("WEAVE_LOGIN_RATE_LIMIT"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				loginRateLimit = n
			}
		}
		// US-253: an in-memory single-use challenge store bridges
		// /api/auth/login (mints) and /api/auth/mfa/verify (consumes). Always
		// constructed so the login handler can short-circuit MFA-enabled
		// users; the verify endpoint is only mounted when MFAStore is wired.
		if deps.MFAChallenges == nil {
			deps.MFAChallenges = auth.NewMFAChallengeStore(auth.DefaultMFAChallengeTTL)
		}
		loginHandler := auth.NewLoginHandler(auth.LoginHandlerDeps{
			Users:          deps.UserRepo,
			Resolver:       deps.RoleResolver,
			Signer:         deps.JWTSigner,
			RefreshService: deps.RefreshService,
			RateLimit:      loginRateLimit,
			MarkingRepo:    markingRepo,
			MFAChallenges:  deps.MFAChallenges,
			Sessions:       deps.SessionStore,
		})
		refreshHandler := auth.NewRefreshHandler(auth.RefreshHandlerDeps{
			Users:          deps.UserRepo,
			Resolver:       deps.RoleResolver,
			Signer:         deps.JWTSigner,
			RefreshService: deps.RefreshService,
			Sessions:       deps.SessionStore,
		})
		logoutHandler := auth.NewLogoutHandler(deps.RefreshService, nil)
		r.Method(http.MethodPost, "/api/auth/login", loginHandler)
		r.Method(http.MethodPost, "/api/auth/refresh", refreshHandler)
		r.Method(http.MethodPost, "/api/auth/logout", logoutHandler)

		// US-246: OIDC SSO front-door. Mounted alongside password login so
		// operators can offer both; nil means the OIDC discovery failed at
		// boot (unreachable issuer / misconfig) and the endpoints are not
		// registered.
		if deps.OIDCHandler != nil {
			deps.OIDCHandler.RegisterRoutes(r)
		}
		// US-255: OIDC back-channel logout. Mounted only when the OIDC
		// front-door is up AND the session/refresh stores exist — bulk
		// revocation has no observable effect without them.
		if deps.OIDCLogoutHandler != nil {
			deps.OIDCLogoutHandler.RegisterRoutes(r)
		}

		// US-248: SAML SSO front-door. Mounted alongside password login /
		// OIDC so operators can mix and match; nil means the IdP cert was
		// missing or unparseable at boot and the endpoints are skipped.
		if deps.SAMLHandler != nil {
			deps.SAMLHandler.RegisterRoutes(r)
		}
		// US-255: SAML Single Logout. Mounted only when the SAML
		// front-door is up; reuses the same gosaml2 SP for signature
		// verification + LogoutResponse construction.
		if deps.SAMLSLOHandler != nil {
			deps.SAMLSLOHandler.RegisterRoutes(r)
		}

		// US-253: MFA endpoints. Mounted alongside password login when both
		// MFAStore and MFAChallenges are wired; in degraded mode (no PG)
		// MFAStore is nil and the routes are skipped.
		if deps.MFAStore != nil && deps.MFAChallenges != nil {
			mfaHandler := auth.NewMFAHandler(auth.MFAHandlerDeps{
				Users:          deps.UserRepo,
				MFAStore:       deps.MFAStore,
				Resolver:       deps.RoleResolver,
				Signer:         deps.JWTSigner,
				RefreshService: deps.RefreshService,
				MFAChallenges:  deps.MFAChallenges,
				MarkingRepo:    markingRepo,
				Sessions:       deps.SessionStore,
			})
			mfaHandler.RegisterRoutes(r)
		}
	}

	// US-132: WebSocket subscription endpoint. Mounted OUTSIDE the auth
	// middleware group because WebSocket clients cannot set HTTP headers
	// during the upgrade handshake; auth is performed via the ?token=
	// query parameter in the Handler before upgrade.
	if deps.WebSocketHub != nil {
		var validator subscriptions.TokenValidator
		if deps.JWTSigner != nil {
			signer := deps.JWTSigner
			validator = func(token string) (string, error) {
				if token == "" {
					// Dev mode: allow empty token
					if os.Getenv("AUTH_MODE") == "" || os.Getenv("AUTH_MODE") == "dev" {
						return "dev-user", nil
					}
					return "", subscriptions.ErrInvalidToken
				}
				claims, err := signer.Verify(token)
				if err != nil {
					// Dev mode: accept raw token as user ID
					if os.Getenv("AUTH_MODE") == "" || os.Getenv("AUTH_MODE") == "dev" {
						return token, nil
					}
					return "", subscriptions.ErrInvalidToken
				}
				return claims.Subject, nil
			}
		}
		wsHandler := subscriptions.NewHandler(deps.WebSocketHub, validator)
		r.Get("/api/v2/ontologies/{ontologyApiName}/subscriptions/ws", wsHandler.ServeHTTP)
	}

	// US-142: OAuth 2.0 endpoints — /oauth/authorize (GET+POST) and
	// /oauth/token (POST). Mounted at the ROOT, not under /api/v2/, to
	// match OAuth conventions third-party clients expect. The consent
	// screen itself does not require an authenticated user in dev mode;
	// in production the router will layer the auth middleware on it.
	var oauthValidator auth.OAuthTokenValidator
	if deps.ApplicationRepo != nil && deps.AuthCodeRepo != nil && deps.OAuthTokenRepo != nil {
		oauthHandler := developer.NewOAuthHandler(deps.ApplicationRepo, deps.AuthCodeRepo, deps.OAuthTokenRepo)
		oauthHandler.RegisterRoutes(r)
		oauthValidator = developer.NewOAuthAuthenticator(deps.OAuthTokenRepo)
	}

	// Auth-protected API routes
	r.Group(func(api chi.Router) {
		// MiddlewareFull unifies JWT, wvk_ api-key, and wvoa_ OAuth bearer
		// auth. Any optional dependency can be nil (dev / minimal test
		// harness): the middleware degrades gracefully. A nil
		// oauthValidator falls through to MiddlewareWithAPIKeys.
		api.Use(auth.MiddlewareFull(
			deps.JWTSigner,
			deps.APIKeyRepo,
			deps.UserRepo,
			deps.RoleResolver,
			oauthValidator,
		))

		// US-044: enforce per-ontology scope on every route that carries an
		// {ontologyApiName} URL param. Dev mode injects an admin user so this
		// middleware is a no-op for the existing dev surface; in jwt mode it
		// rejects requests where the caller has no role for the target
		// ontology. Routes without an {ontologyApiName} param (auth/me, sql
		// queries, attachments) skip the check because there is nothing to
		// scope to.
		api.Use(auth.OntologyScopeMiddleware(auth.PermObjectRead))

		// US-144: per-app API usage metrics. Mounted INSIDE the auth group
		// so the OAuth client_id on User.Attributes is populated when the
		// middleware reads it; dev / JWT / API-key callers fall back to the
		// "anonymous" app_id label.
		api.Use(metrics.UsageMiddleware(deps.UsageSamples))

		// Current-user endpoint (RBAC Phase 1)
		api.Method(http.MethodGet, "/api/v2/me", auth.MeHandler())

		// US-254: active-session inventory. Mounted inside the auth group so
		// only authenticated callers can enumerate/revoke their own sessions.
		// When SessionStore is nil (degraded mode / tests) the routes are
		// skipped so the endpoint surface stays honest.
		if deps.SessionStore != nil {
			sessionHandler := auth.NewSessionHandler(auth.SessionHandlerDeps{
				Sessions:       deps.SessionStore,
				RefreshService: deps.RefreshService,
				AuditStore:     deps.AuditStore,
			})
			sessionHandler.RegisterRoutes(api)
		}

		// OMS routes
		if deps.OmsRepo != nil {
			omsHandler := oms.NewOMSHandler(deps.OmsRepo)
			if deps.LinkPropertyStore != nil {
				omsHandler.SetLinkPropertyStore(deps.LinkPropertyStore)
			}
			if deps.LinkEdgeStore != nil {
				omsHandler.SetLinkEdgeStore(deps.LinkEdgeStore)
			}
			if deps.InterfaceMethodStore != nil {
				omsHandler.SetInterfaceMethodStore(deps.InterfaceMethodStore)
			}
			if deps.InterfaceMethodDispatcher != nil {
				omsHandler.SetInterfaceMethodDispatcher(deps.InterfaceMethodDispatcher)
			}
			RegisterRoutes(api, omsHandler)
		}

		// OSS routes
		if deps.OssSvc != nil {
			ossHandler := oss.NewHandler(deps.OssSvc)
			if deps.AggEngine != nil && deps.IndexMgr != nil {
				ossHandler.SetAggregation(deps.AggEngine, deps.IndexMgr)
			}
			// US-049: same adapter that objSetHandler uses for load paths —
			// gates /objects/{type}/aggregate against AllowedProperties so
			// column-level secrecy cannot be bypassed via aggregation.
			if deps.PolicyEngine != nil && deps.OmsRepo != nil {
				ossHandler.SetPropertyFilterProvider(newPropertyFilterAdapter(deps.OmsRepo, deps.PolicyEngine))
			}
			if deps.OmsRepo != nil {
				ossHandler.SetOmsRepo(deps.OmsRepo)
			}
			if deps.AttachmentStore != nil {
				ossHandler.SetAttachmentStore(deps.AttachmentStore)
			}
			if deps.TimeSeriesStore != nil {
				ossHandler.SetTimeSeriesStore(deps.TimeSeriesStore)
			}
			if deps.GeotemporalStore != nil {
				ossHandler.SetGeotemporalStore(deps.GeotemporalStore)
			}
			if deps.CipherDecryptor != nil {
				ossHandler.SetCipherDecryptor(deps.CipherDecryptor)
			}
			ossHandler.RegisterRoutes(api)
		}

		// Action routes — Foundry OSv2 shape: the action API name is
		// carried in the URL, not in the request body.
		//   POST /api/v2/ontologies/{ontology}/actions/{action}/apply
		//   POST /api/v2/ontologies/{ontology}/actions/{action}/applyBatch
		// cf. palantir/foundry-platform-python action.py L58/L148.
		if deps.ActionExecutor != nil {
			actionHandler := actions.NewHandler(deps.ActionExecutor)
			api.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/apply", actionHandler.Apply)
			api.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/applyBatch", actionHandler.ApplyBatch)
			api.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/applyWithOverrides", actionHandler.ApplyWithOverrides)
			api.Post("/api/v2/ontologies/{ontologyApiName}/actions/revert", actionHandler.Revert)
			// US-240: async-apply polling endpoint. Always registered when the
			// executor is wired; returns 404 if no job store is attached.
			api.Get("/api/v2/ontologies/{ontologyApiName}/actions/jobs/{jobId}", actionHandler.GetJob)
			// US-242: approval-workflow endpoints. Always registered when the
			// executor is wired; return 404 if no approval store is attached.
			// US-243: ListApprovals backs the /approvals UI page.
			api.Get("/api/v2/ontologies/{ontologyApiName}/actions/approvals", actionHandler.ListApprovals)
			api.Post("/api/v2/ontologies/{ontologyApiName}/actions/approvals/{approvalId}/approve", actionHandler.ApproveAction)
			api.Post("/api/v2/ontologies/{ontologyApiName}/actions/approvals/{approvalId}/reject", actionHandler.RejectAction)
		}

		// US-061/062: Stream ingest endpoint — bypasses Action rules, publishes
		// directly to NATS. Gated behind PermStreamIngest (RBAC) and the policy
		// engine (row-level ABAC via AllowedForIngest). The ingest-writer role
		// added in US-062 grants PermStreamIngest without the broader privileges
		// of ontology-owner / admin.
		if deps.FunnelPublisher != nil {
			ingestHandler := oss.NewStreamIngestHandler(deps.FunnelPublisher)
			if deps.PolicyEngine != nil {
				ingestHandler.SetPolicyChecker(
					newIngestPolicyAdapter(deps.OmsRepo, deps.PolicyEngine))
			}
			// US-063: per-ontology token-bucket rate limiter for stream ingest.
			if deps.IngestRateLimiter != nil {
				ingestHandler.SetRateLimiter(deps.IngestRateLimiter)
			}
			api.With(auth.RequirePermission(auth.PermStreamIngest)).
				Method(http.MethodPost,
					"/api/v2/ontologies/{ontologyApiName}/streams/{objectType}/ingest",
					ingestHandler)
		}

		// Attachment endpoints (global — no {ontology} segment).
		if deps.AttachmentStore != nil {
			attachmentHandler := attachment.NewHandler(deps.AttachmentStore)
			attachmentHandler.RegisterRoutes(api)
		}

		// US-204: Media upload/download/delete endpoints. Mounted only when
		// both the content-addressed Store and the MediaAssetStore catalog
		// are wired; degraded mode without PG silently skips registration.
		if deps.MediaStore != nil && deps.MediaCatalog != nil {
			mediaHandler := media.NewHandler(deps.MediaStore, deps.MediaCatalog)
			mediaHandler.RegisterRoutes(api)
		}

		// OntologyTransaction experimental edits endpoint (US-041).
		// Gated behind ?preview=true — only "append edits" is exposed.
		if deps.TransactionStore != nil {
			txnHandler := transactions.NewHandler(deps.TransactionStore)
			txnHandler.RegisterRoutes(api)
		}

		// SqlQueries.execute (US-042). Foundry top-level resource — NOT
		// nested under /ontologies/. Engine may be nil in degraded mode;
		// the handler reports SqlQueryEngineNotConfigured in that case so
		// the route is always documented and discoverable.
		sqlQueryHandler := sqlqueries.NewHandler(deps.SqlQueryEngine)
		sqlQueryHandler.RegisterRoutes(api)

		// ObjectSet endpoints
		if deps.ObjSetExecutor != nil && deps.IndexMgr != nil && deps.ObjSetStore != nil {
			objSetHandler := objectset.NewHandler(deps.ObjSetExecutor, deps.IndexMgr, deps.ObjSetStore)
			// US-048: wire column-level visibility through the same Engine
			// as the row-level PolicyQueryProvider. Nil engine / repo is
			// tolerated — the adapter becomes a no-op and LoadObjects keeps
			// returning full property payloads.
			if deps.PolicyEngine != nil && deps.OmsRepo != nil {
				objSetHandler.SetPropertyFilterProvider(newPropertyFilterAdapter(deps.OmsRepo, deps.PolicyEngine))
			}
			// US-223: wire the time-travel snapshot provider when both an
			// OMS repo (for ObjectType resolution) and the uncached
			// *PGRepository (which exposes SnapshotObjectsAt) are available.
			// Degraded-mode test routers without PG leave the hook unset and
			// asOf requests fall through to TimeTravelUnavailable 400.
			if deps.OmsRepo != nil && deps.HistorySnapshotStore != nil {
				objSetHandler.SetHistorySnapshotProvider(newHistorySnapshotAdapter(deps.OmsRepo, deps.HistorySnapshotStore))
			}
			// US-224: persisted ObjectSet snapshots. Wire only when a
			// PG-backed catalog is available; without it the snapshot routes
			// degrade to SnapshotsUnavailable 400 instead of materialising
			// in-memory rows that can never be read back.
			if deps.ObjectSetSnapshotStore != nil {
				objSetHandler.SetPersistedSnapshotStore(newObjectSetSnapshotAdapter(deps.ObjectSetSnapshotStore))
			}
			// US-264: hook loadObjectSet into the data-access auditor so
			// reads against audit-opted-in ObjectTypes produce an
			// audit_events row (action = "data.access"). Adapter resolves
			// the target ObjectType via the OMS repo so the handler stays
			// oblivious to the flag check.
			if deps.OmsRepo != nil && deps.AuditStore != nil {
				objSetHandler.SetDataAccessAuditor(
					newLoadObjectSetAuditAdapter(deps.OmsRepo, oss.NewDataAccessAuditor(deps.AuditStore)),
				)
			}
			api.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", objSetHandler.LoadObjects)
			api.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadLinks", objSetHandler.LoadLinks)
			api.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/aggregate", objSetHandler.Aggregate)
			api.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/createTemporary", objSetHandler.CreateTemporary)
			// Foundry preview endpoints: multi-type + interface-scoped loads.
			// Register these BEFORE the wildcard /{objectSetRid} so chi does
			// not swallow the static path segments.
			api.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsMultipleObjectTypes", objSetHandler.LoadObjectsMultipleObjectTypes)
			api.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsOrInterfaces", objSetHandler.LoadObjectsOrInterfaces)
			// US-224: snapshot routes. POST /{objectSetRid}/snapshot freezes
			// an ObjectSet; GET /snapshots/{snapshotRid} returns the frozen
			// rows. The static "snapshots" segment must be registered BEFORE
			// the GET wildcard /{objectSetRid} so chi resolves the prefix
			// match correctly.
			api.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/snapshot", objSetHandler.CreateSnapshot)
			api.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/snapshots/{snapshotRid}", objSetHandler.GetSnapshot)
			api.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}", objSetHandler.GetObjectSet)

			// US-055: SSE ObjectSet subscribe scaffold. Wire only when a
			// broadcaster is present — degraded mode (no NATS) leaves the
			// field nil and the route falls through to chi's 404 just like
			// any unwired optional subsystem.
			if deps.FunnelBroadcast != nil {
				sseHandler := oss.NewSubscribeSSEHandler(
					newObjectSetLookupAdapter(deps.ObjSetStore),
					deps.FunnelBroadcast,
				)
				api.Get("/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/subscribe", sseHandler.ServeHTTP)
			}
		}

		// Admin: index rebuild (US-011). Gated to admin-level roles via
		// PermUserManage — it reindexes Bleve from the authoritative
		// object_history tail and is a disruptive, operator-only action.
		api.With(auth.RequirePermission(auth.PermUserManage)).
			Method(http.MethodPost, "/api/admin/indexes/rebuild", NewAdminIndexRebuildHandler(AdminIndexRebuildDeps{
				IndexMgr:  deps.IndexMgr,
				Repo:      deps.OmsRepo,
				DocSource: deps.IndexDocSource,
			}))

		// Admin: audit events (US-067). Gated to admin-level roles via
		// PermUserManage. The handler gracefully returns 503 when
		// AuditStore is nil (no PG pool / degraded mode).
		api.With(auth.RequirePermission(auth.PermUserManage)).
			Method(http.MethodGet, "/api/v2/admin/auditEvents", NewAdminAuditEventsHandler(deps.AuditStore))

		// Developer Console: OAuth application registration (US-141). Any
		// authenticated user can register apps; the handler enforces per-row
		// ownership so callers only see / mutate their own rows.
		if deps.ApplicationRepo != nil {
			appHandler := developer.NewApplicationHandler(deps.ApplicationRepo)
			appHandler.RegisterRoutes(api)

			// US-144: per-app usage metrics endpoint sits alongside the
			// registration CRUD and reuses the same ownership check. The
			// sample store may be nil (degraded mode) — the handler returns
			// empty windows in that case.
			usageHandler := developer.NewUsageHandler(deps.ApplicationRepo, deps.UsageSamples)
			usageHandler.RegisterRoutes(api)
		}

		// US-249: Service accounts admin CRUD. Gated behind PermUserManage so
		// only admin-level roles can mint / list / revoke non-interactive
		// principals. Degraded mode without PG leaves the repo nil and the
		// routes are not mounted so test routers don't see mystery 5xxs.
		if deps.ServiceAccountRepo != nil {
			saHandler := auth.NewServiceAccountHandler(deps.ServiceAccountRepo, deps.AuditStore)
			api.With(auth.RequirePermission(auth.PermUserManage)).
				Group(func(admin chi.Router) {
					saHandler.RegisterRoutes(admin)
				})
		}

		// US-251: Groups / Roles / User-role admin CRUD. Gated behind
		// PermUserManage. Each handler mounts conditionally on its own
		// repo so partially-wired deployments (e.g. unit-test routers)
		// don't trip mystery 5xxs. User-role grant+revoke requires BOTH
		// the users repo (for existence check + grant) and the roles
		// repo (for registry lookup), so it gates on both.
		if deps.GroupRepo != nil {
			groupHandler := auth.NewGroupHandler(deps.GroupRepo, deps.AuditStore)
			api.With(auth.RequirePermission(auth.PermUserManage)).
				Group(func(admin chi.Router) {
					groupHandler.RegisterRoutes(admin)
				})
		}
		if deps.RoleRepo != nil {
			roleHandler := auth.NewRoleHandler(deps.RoleRepo, deps.AuditStore)
			api.With(auth.RequirePermission(auth.PermUserManage)).
				Group(func(admin chi.Router) {
					roleHandler.RegisterRoutes(admin)
				})
		}
		if deps.UserRepo != nil && deps.RoleRepo != nil {
			revoker, _ := deps.UserRepo.(auth.UserRoleRevoker)
			userRoleHandler := auth.NewUserRoleHandler(deps.UserRepo, deps.RoleRepo, revoker, deps.AuditStore)
			api.With(auth.RequirePermission(auth.PermUserManage)).
				Group(func(admin chi.Router) {
					userRoleHandler.RegisterRoutes(admin)
				})
		}

		// US-259: Marking grant admin CRUD. Gates behind PermUserManage
		// alongside the other identity-surface admin handlers. Mounts only
		// when the marking repo is wired (PG mode); degraded-mode routers
		// skip registration so the contract-test chi.Walk never sees the
		// routes. MarkingAdminRepo may be nil if only the request-hot-path
		// MarkingRepository is wired — the handler surfaces a structured
		// 500 on the list-grants endpoints in that case.
		if deps.MarkingRepo != nil {
			markingHandler := auth.NewMarkingHandler(deps.MarkingRepo, deps.MarkingAdminRepo, deps.UserRepo, deps.AuditStore)
			api.With(auth.RequirePermission(auth.PermUserManage)).
				Group(func(admin chi.Router) {
					markingHandler.RegisterRoutes(admin)
				})
		}

		// US-256: Row-Level Security admin CRUD. Mounts the
		// /api/admin/row-policies surface when the RowPolicyStore is wired
		// (PG mode). The handler is given the RowPolicyEngine pointer so
		// writes trigger an in-process cache refresh without waiting for
		// the next full Reload.
		if deps.RowPolicyStore != nil {
			rlsHandler := rls.NewHandler(deps.RowPolicyStore, deps.AuditStore, deps.RowPolicyEngine)
			api.With(auth.RequirePermission(auth.PermUserManage)).
				Group(func(admin chi.Router) {
					rlsHandler.RegisterRoutes(admin)
				})
		}

		// US-257: Column-Level Masking admin CRUD. Mounts
		// /api/admin/column-masks when ColumnMaskStore is wired. Handler
		// receives ColumnMaskEngine so writes refresh the in-process
		// cache immediately.
		if deps.ColumnMaskStore != nil {
			maskHandler := masking.NewHandler(deps.ColumnMaskStore, deps.AuditStore, deps.ColumnMaskEngine)
			api.With(auth.RequirePermission(auth.PermUserManage)).
				Group(func(admin chi.Router) {
					maskHandler.RegisterRoutes(admin)
				})
		}

		// US-258: Cell-Level Security admin CRUD. Mounts
		// /api/admin/cell-masks when CellMaskStore is wired. Handler
		// receives CellMaskEngine so writes refresh the in-process
		// cache immediately.
		if deps.CellMaskStore != nil {
			cellHandler := cellsec.NewHandler(deps.CellMaskStore, deps.AuditStore, deps.CellMaskEngine)
			api.With(auth.RequirePermission(auth.PermUserManage)).
				Group(func(admin chi.Router) {
					cellHandler.RegisterRoutes(admin)
				})
		}

		// US-267: GDPR right-to-be-forgotten async erase. Mounts when
		// the GDPRJobStore is wired (PG mode). The orchestrator's step
		// list is composed from existing services — sessions /
		// refresh tokens / user identity / audit redaction. Each step
		// degrades gracefully when its dependency is nil so partially-
		// wired deployments still produce a sensible job result.
		if deps.GDPRJobStore != nil {
			pgUser, _ := deps.UserRepo.(*auth.PGUserRepository)
			steps := []gdpr.Step{
				gdpr.NewSessionStep(deps.SessionStore),
				gdpr.NewRefreshStep(deps.RefreshService),
				gdpr.NewUserStep(pgUser),
				gdpr.NewAuditRedactionStep(deps.GDPRRedactions),
			}
			eraser := gdpr.NewEraser(deps.GDPRJobStore, steps)
			gdprHandler := gdpr.NewHandler(deps.GDPRJobStore, eraser, deps.AuditStore)
			if deps.GDPRExporter != nil {
				gdprHandler.SetExporter(deps.GDPRExporter)
			}
			api.With(auth.RequirePermission(auth.PermUserManage)).
				Group(func(admin chi.Router) {
					gdprHandler.RegisterRoutes(admin)
				})
		} else if deps.GDPRExporter != nil {
			// Export-only mode: no erase infrastructure but an exporter is
			// wired. Mount just the export endpoint so data portability
			// works in read-only-audit deployments.
			gdprHandler := gdpr.NewHandler(nil, nil, deps.AuditStore)
			gdprHandler.SetExporter(deps.GDPRExporter)
			api.With(auth.RequirePermission(auth.PermUserManage)).
				Group(func(admin chi.Router) {
					gdprHandler.RegisterRoutes(admin)
				})
		}

		// US-270: Compliance control-evidence report. Mounts when the
		// compliance generator has at least one wired source — a fully
		// degraded deployment with zero sources leaves the generator nil
		// and the route is not registered so test routers don't see a
		// mystery handler.
		if deps.ComplianceGenerator != nil {
			complianceHandler := compliance.NewHandler(deps.ComplianceGenerator, deps.AuditStore)
			api.With(auth.RequirePermission(auth.PermUserManage)).
				Group(func(admin chi.Router) {
					complianceHandler.RegisterRoutes(admin)
				})
		}

		// US-276: Feature Flags admin CRUD. Mounts only when the backing
		// store is wired (PG mode); degraded-mode deployments leave the
		// /api/admin/feature-flags/* routes unregistered so featureflags.HasFlag
		// fails closed everywhere.
		if deps.FeatureFlagStore != nil {
			ffHandler := featureflags.NewHandler(deps.FeatureFlagStore)
			api.With(auth.RequirePermission(auth.PermUserManage)).
				Group(func(admin chi.Router) {
					ffHandler.RegisterRoutes(admin)
				})
		}

		// US-277: Tenant Quotas admin CRUD. Mounts only when the backing
		// store is wired (PG mode); degraded-mode deployments leave the
		// /api/admin/tenant-quotas/* routes unregistered.
		if deps.TenantQuotaStore != nil {
			tqHandler := tenants.NewHandler(deps.TenantQuotaStore, deps.TenantQuotaManager)
			api.With(auth.RequirePermission(auth.PermUserManage)).
				Group(func(admin chi.Router) {
					tqHandler.RegisterRoutes(admin)
				})
		}

		// US-279: AIP Threads CRUD + messages. Mounts only when the
		// backing store is wired (PG mode); degraded-mode deployments
		// leave the /api/v2/aip/threads/* routes unregistered. The
		// registry may be nil (e.g. fully offline test rigs) in which
		// case SendMessage emits a structured AIPProviderNotConfigured
		// 500 instead of dispatching.
		if deps.AIPStore != nil {
			aipHandler := aip.NewHandler(deps.AIPStore, deps.AIPRegistry)
			aipHandler.SetToolRegistry(deps.AIPTools)
			aipHandler.RegisterRoutes(api)
		}

		// US-285: AIP custom tool catalog. Mounts the /api/v2/aip/tools
		// admin CRUD endpoints when the backing catalog is wired (PG
		// mode). The handler also keeps the in-process AIPTools
		// registry in sync on every Create/Update/Delete so the next
		// SendMessage iteration sees the change without a process
		// restart.
		if deps.AIPToolCatalog != nil {
			toolCatalogHandler := aip.NewToolCatalogHandler(deps.AIPToolCatalog, deps.AIPTools, deps.AIPToolInvoker)
			toolCatalogHandler.RegisterRoutes(api)
		}

		// US-281: AIP Logic Flows. Mounts only when the backing flow
		// store is wired (PG mode). The executor pairs the same
		// AIPRegistry used by /threads with a tool registry so flow
		// authors can mix LLM calls with side-effect tools.
		if deps.AIPLogicStore != nil {
			tools := deps.AIPLogicTools
			if tools == nil {
				tools = aiplogic.NewMapToolRegistry()
			}
			executor := aiplogic.NewExecutor(deps.AIPRegistry, tools)
			logicHandler := aiplogic.NewHandler(deps.AIPLogicStore, executor)
			logicHandler.RegisterRoutes(api)
		}
	})

	return r
}

// NewServer creates an http.Server with production timeouts.
func NewServer(handler http.Handler, port int) *http.Server {
	return &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}

// shutdownableServer is the minimal subset of *http.Server needed by
// gracefulShutdown so the function is unit-testable with a fake.
type shutdownableServer interface {
	Shutdown(ctx context.Context) error
}

// stoppableConsumer is the minimal subset of *funnel.Consumer needed by
// gracefulShutdown so the function is unit-testable with a fake.
type stoppableConsumer interface {
	Stop() error
}

// gracefulShutdown stops the HTTP server first (so no new edits enter the
// pipeline) and then stops the funnel consumer (releasing the NATS
// subscription cleanly). The consumer is stopped even if the HTTP shutdown
// errors so the NATS subscription does not leak. The first non-nil error is
// returned to the caller.
func gracefulShutdown(ctx context.Context, srv shutdownableServer, consumer stoppableConsumer) error {
	var firstErr error
	if srv != nil {
		if err := srv.Shutdown(ctx); err != nil {
			firstErr = err
		}
	}
	if consumer != nil {
		if err := consumer.Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("config validation failed: %v", err)
	}

	// Initialize structured logging. All subsequent log.Printf calls in this
	// function continue to work (slog runs in parallel via slog.SetDefault).
	logger := InitLogger(cfg, os.Stderr)
	slog.SetDefault(logger)
	slog.Info("starting Weave", "port", cfg.Port, "auth_mode", cfg.AuthMode)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// US-271: OpenTelemetry tracer provider. Init is a no-op when
	// Tracing.Enabled is false, so the rest of the server can use
	// otel.Tracer / pkg/tracing.StartSpan unconditionally. The
	// returned shutdown flushes any pending OTLP/stdout exports during
	// graceful shutdown.
	tracingShutdown, err := tracing.Init(ctx, tracing.Config{
		Enabled:        cfg.Tracing.Enabled,
		Exporter:       cfg.Tracing.Exporter,
		OTLPEndpoint:   cfg.Tracing.OTLPEndpoint,
		OTLPProtocol:   cfg.Tracing.OTLPProtocol,
		OTLPInsecure:   cfg.Tracing.OTLPInsecure,
		ServiceName:    cfg.Tracing.ServiceName,
		ServiceVersion: os.Getenv("WEAVE_VERSION"),
	})
	if err != nil {
		log.Fatalf("tracing init: %v", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := tracingShutdown(shutdownCtx); err != nil {
			log.Printf("tracing shutdown: %v", err)
		}
	}()

	deps := &ServerDeps{
		CORSOrigins: cfg.CORSOrigins,
	}

	// 1. PostgreSQL
	if cfg.PGDSN != "" {
		// US-271: install the pgx QueryTracer ONLY when tracing is
		// enabled — the no-op tracer would still allocate a span per
		// query, which is not free for low-latency reads.
		var pgTracer pgx.QueryTracer
		if cfg.Tracing.Enabled {
			pgTracer = tracing.NewPgxTracer()
		}
		pool, err := database.ConnectWithTracer(ctx, cfg.PGDSN, database.DefaultPoolConfig(), pgTracer)
		if err != nil {
			log.Fatalf("database connect: %v", err)
		}
		defer pool.Close()
		deps.PGPool = pool

		if err := database.RunMigrationsUp(cfg.PGDSN, "migrations"); err != nil {
			log.Printf("warning: migration failed: %v", err)
		}

		// Wrap the raw PG repository with a 60s TTL cache decorator so that
		// hot metadata reads (GetOntology / GetObjectTypeByAPIName /
		// GetLinkType / ListOutgoingLinkTypes / ...) don't hit PostgreSQL
		// on every request. Writes through the decorator invalidate all
		// caches; external invalidation is available via InvalidateAll.
		pgRepo := oms.NewPGRepository(pool)
		deps.OmsRepo = oms.NewCachedRepository(pgRepo, 60*time.Second)
		// US-204: Media catalog is served by the uncached *PGRepository — the
		// metadata cache decorator does not wrap MediaAssetStore methods, and
		// upload/delete are infrequent enough that direct PG hits are fine.
		deps.MediaCatalog = pgRepo
		// US-210: link-property schema + link-edge value stores. Same reason
		// as MediaCatalog — these narrow stores are not on oms.Repository, so
		// the CachedRepository decorator does not wrap them.
		deps.LinkPropertyStore = pgRepo
		deps.LinkEdgeStore = pgRepo
		// US-214: interface_methods CRUD + ActionType.implementsMethodRid
		// validation. Served by the uncached *PGRepository (the
		// CachedRepository decorator wraps the oms.Repository surface only).
		deps.InterfaceMethodStore = pgRepo
		// US-223: time-travel snapshot reader. Served by the uncached
		// *PGRepository for the same reason — SnapshotObjectsAt is not on
		// the oms.Repository surface that the cache decorator wraps.
		deps.HistorySnapshotStore = pgRepo
		// US-224: persisted ObjectSet snapshots. Same pattern — the
		// Create/GetObjectSetSnapshot methods live on *PGRepository, not
		// on the cached metadata Repository.
		deps.ObjectSetSnapshotStore = pgRepo
		// US-067: PG-backed audit event store for the admin read endpoint.
		// US-265: optional SIEM exporter tees every persisted event onto
		// stdout/syslog/S3 via a BatchedExporter. Disabled by default so
		// fresh deployments don't wake up writing to an un-configured
		// destination; enable with WEAVE_AUDIT_EXPORT_KIND.
		// US-266: the PG store now writes a tamper-proof hash chain on
		// every Insert (chain_seq / prev_hash / entry_hash columns). An
		// optional root-hash publisher (enabled via
		// WEAVE_AUDIT_ROOTHASH_FILE) anchors the previous UTC day's chain
		// root to an append-only file every interval — operators run
		// `weave-audit-verify -root-file <path>` to cross-check.
		// US-267: GDPR right-to-be-forgotten. Two PG-backed stores:
		//   gdpr_erasure_jobs — async job state, polled by the SDK/UI
		//   gdpr_redactions   — audit-PII overlay applied at audit List time
		// The redaction store wraps the audit Store via a RedactingStore
		// decorator so erased actor_ids see their PII scrubbed without
		// breaking the US-266 hash chain.
		deps.GDPRJobStore = newPGGDPRJobStore(pool)
		deps.GDPRRedactions = newPGGDPRRedactionStore(pool)

		pgAudit := audit.NewPGStore(pool)
		// Tee any optional SIEM exporter THEN wrap in the GDPR redaction
		// overlay. Order matters: the exporter sees the unredacted event
		// (operators ship full audit to SIEM under separate retention
		// rules), the API surface sees the redacted view.
		deps.AuditStore = audit.NewRedactingStore(
			newAuditStoreWithExport(cfg.AuditExport, pgAudit),
			deps.GDPRRedactions,
		)
		if cfg.AuditExport.RootHashFile != "" {
			pub := audit.NewRootHashPublisher(pgAudit, cfg.AuditExport.RootHashFile)
			pub.SetInterval(cfg.AuditExport.RootHashInterval)
			pub.Start(ctx)
			defer pub.Stop()
			log.Printf("audit root-hash publisher enabled: file=%s interval=%s",
				cfg.AuditExport.RootHashFile, cfg.AuditExport.RootHashInterval)
		}
		// US-269: archive-and-delete retention policy. startAuditRetention
		// returns nil when RetentionDays<=0; the PG store directly
		// implements retention.Store so no adapter is needed. Operators
		// who want S3 archive must additionally supply an S3Uploader
		// (pluggable SDK pattern from US-265) — without one the sweep
		// runs in delete-only mode.
		if sched := startAuditRetention(ctx, cfg.AuditExport, pgAudit, nil); sched != nil {
			defer sched.Stop()
		}
		// US-011: the index rebuild admin command re-ingests from
		// object_history. Keep the uncached *PGRepository reference so the
		// rebuild path always observes the authoritative tail.
		deps.IndexDocSource = newPGIndexDocSource(pgRepo)
		pgUserRepo := auth.NewPGUserRepository(pool)
		deps.UserRepo = pgUserRepo
		// US-253: MFA persistence is the same uncached *PGUserRepository so
		// reads after enable/disable observe their own writes immediately.
		deps.MFAStore = pgUserRepo
		deps.APIKeyRepo = auth.NewPGAPIKeyRepository(pool)
		// US-249: service accounts share the users FK so the bootstrap
		// ordering is after UserRepo (which runs migrations). The PG
		// implementation is uncached — CRUD volume is low and the
		// name-uniqueness invariant needs fresh reads.
		deps.ServiceAccountRepo = auth.NewPGServiceAccountRepository(pool)
		// US-251: Groups and roles registry. Same uncached pattern — admin
		// CRUD volume is low and both tables back cache-sensitive lookups
		// (group membership, role→permission resolution).
		deps.GroupRepo = auth.NewPGGroupRepository(pool)
		deps.RoleRepo = auth.NewPGRoleRepository(pool)
		// US-256: Row-level policy store (row_policies table). Uncached —
		// admin CRUD is infrequent and the engine's own cache absorbs the
		// hot-path reads at query time.
		deps.RowPolicyStore = newPGRowPolicyStore(pool)
		// US-257: Column-mask store (column_masks table). Uncached for the
		// same reason as RowPolicyStore — admin volume is low, engine owns
		// the hot-path cache.
		deps.ColumnMaskStore = newPGColumnMaskStore(pool)
		// US-258: Cell-mask store (cell_masks table). Same uncached pattern
		// — cell masks are per-(ObjectType, primaryKey, property) and the
		// engine's own index keeps lookup O(1) on the read path.
		deps.CellMaskStore = newPGCellMaskStore(pool)
		// US-259: marking grant admin surface. The concrete
		// *PGMarkingRepository satisfies BOTH the request-hot-path
		// MarkingRepository (used by login / OIDC / SAML JWT enrichment
		// plus the ObjectSet marking filter) AND the new
		// MarkingGrantAdminRepository used by /api/admin/markings. We
		// build one instance and share it so both wiring points see the
		// same pool / cache semantics.
		pgMarkingRepo := auth.NewPGMarkingRepository(pool)
		deps.MarkingRepo = pgMarkingRepo
		deps.MarkingAdminRepo = pgMarkingRepo
		deps.ApplicationRepo = developer.NewPGApplicationRepository(pool)
		deps.AuthCodeRepo = developer.NewPGAuthorizationCodeRepository(pool)
		deps.OAuthTokenRepo = developer.NewPGOAuthTokenRepository(pool)
		// US-144: 30d retention is the widest window the /usage endpoint
		// serves; the per-app cap keeps memory bounded even if a single app
		// bursts above the steady-state rate.
		deps.UsageSamples = metrics.NewUsageSampleStore(30*24*time.Hour, 10000)
		deps.RoleResolver = auth.NewRoleResolver(deps.UserRepo, 5*time.Minute)
		deps.RefreshService = auth.NewRefreshService(
			auth.NewPGRefreshStore(pool),
			auth.RefreshServiceOptions{AbsoluteTTL: cfg.JWT.RefreshTokenTTL},
		)
		// US-254: PG-backed session inventory. Same uncached pattern — admin
		// volume is low and the /api/auth/sessions endpoints need to see their
		// own writes immediately (a brand-new login must appear on the next
		// list call).
		deps.SessionStore = auth.NewPGSessionStore(pool)

		// US-268: GDPR data-portability exporter. Composes over UserRepo +
		// AuditStore + MediaCatalog + MediaStore; each source degrades
		// gracefully so partial deployments still emit a useful bundle.
		// MediaCatalog is wired above (line ~940); AuditStore has already
		// been wrapped with the redaction decorator so the export inherits
		// the same PII-scrub semantics as the admin audit API.
		deps.GDPRExporter = buildGDPRExporter(deps.UserRepo, deps.AuditStore, deps.MediaCatalog, deps.MediaStore)

		// US-270: Compliance report generator. Pulls access statistics
		// from the redacted AuditStore, marking distribution from the
		// shared *PGMarkingRepository (which satisfies both the request-
		// hot-path surface AND the admin grant surface), policy coverage
		// from the three security-surface stores wired above. Leaves the
		// generator non-nil whenever any source is available so the
		// /api/admin/compliance/report route mounts in partial
		// deployments too.
		deps.ComplianceGenerator = buildComplianceGenerator(
			deps.AuditStore,
			deps.MarkingRepo,
			deps.MarkingAdminRepo,
			deps.OmsRepo,
			deps.RowPolicyStore,
			deps.ColumnMaskStore,
			deps.CellMaskStore,
		)

		// US-276: Feature Flags. One uncached PG-backed store feeds
		// both the admin CRUD handlers and the read-side Manager the
		// context middleware stamps onto every request.
		deps.FeatureFlagStore = newPGFeatureFlagsStore(pool)
		deps.FeatureFlagManager = featureflags.NewManager(deps.FeatureFlagStore)

		// US-277: Tenant Quotas. Same shape as feature flags — one
		// PG-backed store powers both the admin CRUD handlers and the
		// read-side Manager the QPS middleware uses to gate every
		// authenticated request.
		deps.TenantQuotaStore = newPGTenantQuotaStore(pool)
		deps.TenantQuotaManager = tenants.NewManager(deps.TenantQuotaStore)

		// US-279: AIP Threads + LLM provider registry. The PG store
		// powers the /api/v2/aip/threads/* CRUD endpoints; the registry
		// is built from environment variables so OPENAI_API_KEY and
		// ANTHROPIC_API_KEY drive which providers register at boot.
		// Mock is always registered so dev / CI deployments can chat
		// without external credentials.
		deps.AIPStore = newPGAIPStore(pool)
		aipReg, aipNames := aip.BuildRegistry(aip.LoadEnvConfig())
		deps.AIPRegistry = aipReg
		// US-284 Function Calling Chain: register the built-in echo
		// tool so the SendMessage loop has at least one resolvable
		// function out of the box. Custom deployments can wrap and
		// extend deps.AIPTools.Register(...) before NewFullRouter.
		deps.AIPTools = aip.NewToolRegistry()
		deps.AIPTools.Register(&aip.EchoToolHandler{})
		// US-285 LLM Tool 扩展: load custom tool definitions from the
		// aip_tools table and register them in the live registry. The
		// FunctionInvoker bridges onto the OMS Function Registry so
		// catalog rows pointing at a Function RID dispatch through the
		// existing FunctionExecutor wiring (when present). Catalog rows
		// with no FunctionExecutor wired surface a clean
		// AIPToolHandlerNotConfigured at execute-time.
		deps.AIPToolCatalog = newPGAIPToolCatalog(pool)
		deps.AIPToolInvoker = newAIPFunctionInvoker(deps.OmsRepo, nil)
		if err := aip.LoadCatalogIntoRegistry(ctx, deps.AIPTools, deps.AIPToolCatalog, deps.AIPToolInvoker); err != nil {
			log.Printf("warning: failed to load AIP tool catalog: %v", err)
		}
		log.Printf("[AIP] thread store wired; providers=%v tools=%v", aipNames, deps.AIPTools.Names())

		// US-281: AIP Logic Flows store + tool registry. Tool registry
		// includes the built-in echo / concat tools out of the box.
		deps.AIPLogicStore = newPGAIPLogicStore(pool)
		deps.AIPLogicTools = aiplogic.NewMapToolRegistry()
		log.Printf("[AIP] logic-flow store wired; tools=%v", deps.AIPLogicTools.(*aiplogic.MapToolRegistry).Names())

		// Bootstrap initial admin from env (idempotent). If a password is also
		// supplied, set it via bcrypt so the user can immediately log in via
		// the JWT login flow.
		if email := os.Getenv("WEAVE_BOOTSTRAP_ADMIN"); email != "" {
			if err := auth.BootstrapAdmin(ctx, deps.UserRepo, email); err != nil {
				log.Printf("warning: bootstrap admin failed: %v", err)
			} else {
				log.Printf("[RBAC] Bootstrapped initial admin: %s", email)
				if pwd := os.Getenv("WEAVE_BOOTSTRAP_ADMIN_PASSWORD"); pwd != "" {
					hash, herr := auth.HashPassword(pwd)
					if herr != nil {
						log.Printf("warning: bootstrap password hash failed: %v", herr)
					} else if serr := deps.UserRepo.SetPassword(ctx, "user:"+email, hash); serr != nil {
						log.Printf("warning: bootstrap password set failed: %v", serr)
					} else {
						log.Printf("[AUTH] Bootstrapped admin password for %s", email)
					}
				}
			}
		}
	}

	// JWT signer setup. If AUTH_MODE=jwt the keys are mandatory; otherwise
	// the signer is optional and login/refresh routes simply do not register.
	if cfg.JWT.PrivateKeyPath != "" || cfg.JWT.PrivateKeyPEM != "" {
		var priv *rsa.PrivateKey
		var pub *rsa.PublicKey
		var lerr error
		if cfg.JWT.PrivateKeyPath != "" {
			priv, pub, lerr = auth.LoadRSAKeysFromFiles(cfg.JWT.PrivateKeyPath, cfg.JWT.PublicKeyPath)
		} else {
			priv, pub, lerr = auth.LoadRSAKeysFromPEM(cfg.JWT.PrivateKeyPEM, cfg.JWT.PublicKeyPEM)
		}
		if lerr != nil {
			if cfg.AuthMode == "jwt" {
				log.Fatalf("[AUTH] FATAL: AUTH_MODE=jwt but key load failed: %v", lerr)
			}
			log.Printf("warning: JWT key load failed: %v", lerr)
		} else {
			signer, serr := auth.NewJWTSigner(priv, pub, auth.JWTSignerOptions{
				Issuer:         cfg.JWT.Issuer,
				Audience:       cfg.JWT.Audience,
				AccessTokenTTL: cfg.JWT.AccessTokenTTL,
			})
			if serr != nil {
				log.Fatalf("[AUTH] FATAL: jwt signer init: %v", serr)
			}
			deps.JWTSigner = signer
			log.Printf("[AUTH] JWT tier B (RS256) enabled, issuer=%s", cfg.JWT.Issuer)
		}
	} else if cfg.AuthMode == "jwt" {
		log.Fatalf("[AUTH] FATAL: AUTH_MODE=jwt but WEAVE_JWT_PRIVATE_KEY_PATH (or _PEM) is not set")
	} else if cfg.AuthMode != "jwt" && deps.JWTSigner == nil {
		// US-081: auto-generate an ephemeral RSA key pair in dev/token mode so
		// the login / refresh / logout endpoints are always available. The key
		// only lives for the lifetime of this process — restarts invalidate all
		// previously issued tokens, which is fine for local dev and E2E tests.
		devPriv, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			log.Printf("[AUTH] warning: dev RSA key generation failed: %v", err)
		} else {
			devSigner, serr := auth.NewJWTSigner(devPriv, &devPriv.PublicKey, auth.JWTSignerOptions{
				Issuer:         cfg.JWT.Issuer,
				Audience:       cfg.JWT.Audience,
				AccessTokenTTL: cfg.JWT.AccessTokenTTL,
			})
			if serr != nil {
				log.Printf("[AUTH] warning: dev JWT signer init failed: %v", serr)
			} else {
				deps.JWTSigner = devSigner
				log.Printf("[AUTH] ephemeral dev RSA key generated — login/refresh/logout endpoints enabled")
			}
		}
	}

	if cfg.AuthMode == "token" {
		log.Printf("[AUTH] WARNING: AUTH_MODE=token is deprecated and accepts unauthenticated tokens. Use AUTH_MODE=jwt in production.")
	}

	// US-246: OIDC front-door. Constructed AFTER the JWT signer + refresh
	// service so the callback path has every collaborator it needs to mint a
	// Weave session once the provider's id_token has been verified. Errors
	// degrade loudly — the process keeps running with OIDC off rather than
	// crashing so operators can still use password login / API keys.
	if cfg.OIDC.Enabled {
		if deps.JWTSigner == nil || deps.RefreshService == nil || deps.UserRepo == nil {
			log.Printf("[OIDC] WARNING: OIDC.Enabled=true but JWT/refresh/user deps missing — /api/auth/oidc/* not mounted")
		} else {
			oidcProvider, err := oidc.NewProvider(ctx, cfg.OIDC.IssuerURL)
			if err != nil {
				log.Printf("[OIDC] WARNING: discovery failed for %s: %v — /api/auth/oidc/* not mounted", cfg.OIDC.IssuerURL, err)
			} else {
				exchanger, verifier := auth.NewOIDCDepsFromProvider(oidcProvider, auth.OIDCConfig{
					IssuerURL:          cfg.OIDC.IssuerURL,
					ClientID:           cfg.OIDC.ClientID,
					ClientSecret:       cfg.OIDC.ClientSecret,
					RedirectURL:        cfg.OIDC.RedirectURL,
					Scopes:             cfg.OIDC.Scopes,
					SuccessRedirectURL: cfg.OIDC.SuccessRedirectURL,
				})
				var markingRepo auth.MarkingRepository
				if deps.PGPool != nil {
					markingRepo = auth.NewPGMarkingRepository(deps.PGPool)
				}
				deps.OIDCHandler = auth.NewOIDCHandler(auth.OIDCHandlerDeps{
					Config: auth.OIDCConfig{
						IssuerURL:          cfg.OIDC.IssuerURL,
						ClientID:           cfg.OIDC.ClientID,
						ClientSecret:       cfg.OIDC.ClientSecret,
						RedirectURL:        cfg.OIDC.RedirectURL,
						Scopes:             cfg.OIDC.Scopes,
						SuccessRedirectURL: cfg.OIDC.SuccessRedirectURL,
					},
					Exchanger:      exchanger,
					Verifier:       verifier,
					Users:          deps.UserRepo,
					Resolver:       deps.RoleResolver,
					Signer:         deps.JWTSigner,
					RefreshService: deps.RefreshService,
					MarkingRepo:    markingRepo,
				})
				// US-255: back-channel logout reuses the same Verifier
				// so IdP key rotations apply to both surfaces in lockstep.
				deps.OIDCLogoutHandler = auth.NewOIDCBackChannelLogoutHandler(auth.OIDCBackChannelLogoutDeps{
					Verifier:       verifier,
					ClientID:       cfg.OIDC.ClientID,
					Users:          deps.UserRepo,
					SessionStore:   deps.SessionStore,
					RefreshService: deps.RefreshService,
				})
				log.Printf("[OIDC] enabled: issuer=%s client_id=%s", cfg.OIDC.IssuerURL, cfg.OIDC.ClientID)
			}
		}
	}

	// US-248: SAML 2.0 SSO front-door. Constructed AFTER the JWT signer +
	// refresh service so the ACS callback has every collaborator it needs to
	// mint a Weave session once the IdP assertion has been verified. Errors
	// degrade loudly — the process keeps running with SAML off rather than
	// crashing, so operators can still use password login / OIDC / API keys.
	if cfg.SAML.Enabled {
		if deps.JWTSigner == nil || deps.RefreshService == nil || deps.UserRepo == nil {
			log.Printf("[SAML] WARNING: SAML.Enabled=true but JWT/refresh/user deps missing — /api/auth/saml/* not mounted")
		} else {
			samlVerifier, err := auth.NewSAMLDepsFromConfig(auth.SAMLConfig{
				IdPSSOURL:          cfg.SAML.IdPSSOURL,
				IdPIssuer:          cfg.SAML.IdPIssuer,
				IdPCertificatePEM:  cfg.SAML.IdPCertificatePEM,
				SPEntityID:         cfg.SAML.SPEntityID,
				SPACSURL:           cfg.SAML.SPACSURL,
				SuccessRedirectURL: cfg.SAML.SuccessRedirectURL,
				AttributeEmail:     cfg.SAML.AttributeEmail,
				AttributeName:      cfg.SAML.AttributeName,
			})
			if err != nil {
				log.Printf("[SAML] WARNING: configuration rejected: %v — /api/auth/saml/* not mounted", err)
			} else {
				var samlMarkingRepo auth.MarkingRepository
				if deps.PGPool != nil {
					samlMarkingRepo = auth.NewPGMarkingRepository(deps.PGPool)
				}
				deps.SAMLHandler = auth.NewSAMLHandler(auth.SAMLHandlerDeps{
					Config: auth.SAMLConfig{
						IdPSSOURL:          cfg.SAML.IdPSSOURL,
						IdPIssuer:          cfg.SAML.IdPIssuer,
						IdPCertificatePEM:  cfg.SAML.IdPCertificatePEM,
						SPEntityID:         cfg.SAML.SPEntityID,
						SPACSURL:           cfg.SAML.SPACSURL,
						SuccessRedirectURL: cfg.SAML.SuccessRedirectURL,
						AttributeEmail:     cfg.SAML.AttributeEmail,
						AttributeName:      cfg.SAML.AttributeName,
					},
					SP:             samlVerifier,
					Users:          deps.UserRepo,
					Resolver:       deps.RoleResolver,
					Signer:         deps.JWTSigner,
					RefreshService: deps.RefreshService,
					MarkingRepo:    samlMarkingRepo,
				})
				// US-255: SAML SLO. The same gosaml2-backed verifier
				// satisfies SAMLLogoutVerifier (it implements both narrow
				// interfaces), so a single instance powers both ACS
				// signature verification AND LogoutRequest verification +
				// LogoutResponse rendering.
				if logoutVerifier, ok := samlVerifier.(auth.SAMLLogoutVerifier); ok {
					deps.SAMLSLOHandler = auth.NewSAMLSLOHandler(auth.SAMLSLOHandlerDeps{
						LogoutVerifier: logoutVerifier,
						Users:          deps.UserRepo,
						SessionStore:   deps.SessionStore,
						RefreshService: deps.RefreshService,
					})
				}
				log.Printf("[SAML] enabled: idp=%s entity_id=%s acs=%s",
					cfg.SAML.IdPIssuer, cfg.SAML.SPEntityID, cfg.SAML.SPACSURL)
			}
		}
	}

	// US-252: LDAP/AD periodic directory sync. Constructed AFTER the PG
	// pool so the sync store can write through. Errors degrade loudly —
	// a misconfigured directory leaves the rest of the server running so
	// password / OIDC / SAML logins keep working.
	if cfg.LDAP.Enabled {
		if deps.PGPool == nil {
			log.Printf("[LDAP] WARNING: LDAP.Enabled=true but no PG pool — directory sync disabled")
		} else {
			ldapCfg := auth.LDAPSyncConfig{
				URL:                  cfg.LDAP.URL,
				BindDN:               cfg.LDAP.BindDN,
				BindPassword:         cfg.LDAP.BindPassword,
				StartTLS:             cfg.LDAP.StartTLS,
				InsecureSkip:         cfg.LDAP.InsecureSkip,
				UserBaseDN:           cfg.LDAP.UserBaseDN,
				UserFilter:           cfg.LDAP.UserFilter,
				UserEmailAttribute:   cfg.LDAP.UserEmailAttribute,
				UserNameAttribute:    cfg.LDAP.UserNameAttribute,
				UserLoginAttribute:   cfg.LDAP.UserLoginAttribute,
				GroupBaseDN:          cfg.LDAP.GroupBaseDN,
				GroupFilter:          cfg.LDAP.GroupFilter,
				GroupNameAttribute:   cfg.LDAP.GroupNameAttribute,
				GroupMemberAttribute: cfg.LDAP.GroupMemberAttribute,
				GroupDescriptionAttr: cfg.LDAP.GroupDescriptionAttr,
			}
			if err := ldapCfg.Validate(); err != nil {
				log.Printf("[LDAP] WARNING: %v — directory sync disabled", err)
			} else {
				ldapStore := auth.NewPGLDAPSyncStore(deps.PGPool)
				ldapSvc := auth.NewLDAPSyncService(ldapCfg, auth.NewGoLDAPClientFactory(ldapCfg), ldapStore)
				ldapSched := auth.NewLDAPSyncScheduler(ldapSvc, cfg.LDAP.Interval)
				ldapSched.Start(ctx)
				defer ldapSched.Stop()
				log.Printf("[LDAP] enabled: url=%s user_base=%s interval=%s",
					cfg.LDAP.URL, cfg.LDAP.UserBaseDN, ldapSched.Interval())
			}
		}
	}

	// 2. Index Manager
	deps.IndexMgr = index.NewManager(cfg.DataDir)
	defer deps.IndexMgr.Close()

	// 2b. Attachment blob store (filesystem backend under WEAVE_DATA_DIR/attachments).
	// Unlinked uploads older than 1h are swept by a background cleanup loop.
	attachmentStore := attachment.NewLocalStore(cfg.DataDir + "/attachments")
	deps.AttachmentStore = attachmentStore
	attachmentStore.StartCleanupLoop(ctx, 10*time.Minute, 1*time.Hour)

	// 2b-2. Media content-addressed blob store (US-204). Layout matches
	// pkg/media: <root>/<realm>/<yyyy>/<mm>/<sha256>. The store survives
	// degraded mode (no PG) so unit/dev runs that don't wire MediaCatalog
	// still get a working filesystem; the HTTP handler is gated on both.
	deps.MediaStore = media.NewStore(cfg.DataDir + "/media")

	// 2c. TimeSeries store. Prefer the PG backend when a pool is wired;
	// fall back to an in-memory store in degraded mode so unit/dev runs
	// that skip PG still get live endpoints.
	if deps.PGPool != nil {
		deps.TimeSeriesStore = timeseries.NewPGStore(deps.PGPool)
	} else {
		deps.TimeSeriesStore = timeseries.NewMemoryStore()
	}

	// 2d. Geotemporal store. In-memory only for now — PostGIS/JSONB backend
	// is deferred per the Phase 4 open question in the PRD.
	deps.GeotemporalStore = geotemporal.NewMemoryStore()

	// 2e. CipherTextProperty decryptor. The WEAVE_CIPHER_KEY env var carries
	// the 32-byte master key; when unset, the decrypt endpoint returns
	// CipherDecryptorNotConfigured (single-machine degraded mode). Swapping
	// in a KMS-backed Decryptor is a matter of replacing this assignment.
	if key := os.Getenv("WEAVE_CIPHER_KEY"); key != "" {
		dec, err := cipher.NewAESGCMDecryptor(key)
		if err != nil {
			log.Printf("[CIPHER] WARNING: WEAVE_CIPHER_KEY invalid: %v — decrypt endpoint disabled", err)
		} else {
			deps.CipherDecryptor = dec
			log.Printf("[CIPHER] AES-256-GCM decryptor enabled")
		}
	}

	// 2f. OntologyTransaction experimental store (US-041). In-memory only —
	// transactions are ephemeral per-process and do NOT survive restarts.
	// The endpoint is gated behind ?preview=true so this is intentional.
	deps.TransactionStore = transactions.NewMemoryStore()

	// 2g. SqlQueries.execute engine (US-042). Wired only when a PG pool is
	// available; in degraded mode the handler reports
	// SqlQueryEngineNotConfigured so the endpoint stays mounted.
	if deps.PGPool != nil {
		deps.SqlQueryEngine = sqlqueries.NewPGEngine(deps.PGPool)
	}

	// 2a. Rehydrate Bleve indexes from PG metadata.
	// Creates empty index shells (with correct mappings) for every ObjectType
	// defined in PG so queries don't fail with "index not found" when WEAVE_DATA_DIR
	// is wiped or a new ObjectType was added but never received writes.
	// Historical object data is NOT restored (NATS JetStream uses WorkQueuePolicy).
	if deps.OmsRepo != nil {
		if err := index.EnsureAllIndexes(ctx, deps.IndexMgr, deps.OmsRepo); err != nil {
			slog.Warn("rehydrate failed", "error", err)
		} else {
			slog.Info("rehydrate complete")
		}
	}

	// 3. Link Resolver
	if deps.OmsRepo != nil {
		resolver := links.NewResolver(deps.OmsRepo, deps.IndexMgr)
		// Cache link-type metadata lookups (GetLinkType / ListOutgoingLinkTypes)
		// with a 60s TTL so repeated traversals across the same links don't
		// re-read the same rows from PostgreSQL.
		resolver.SetLinkTypeCache(links.NewLinkTypeCache(60 * time.Second))
		deps.LinkResolver = resolver
	}

	// 4. OSS Service
	if deps.OmsRepo != nil {
		deps.OssSvc = oss.NewService(deps.OmsRepo, deps.IndexMgr, deps.LinkResolver)
	}

	// 4b. Row-level Policy Engine (US-046). The engine is shared between the
	// OSS service's Load/Search paths and the ObjectSet executor's
	// base/filter paths so every read surface sees the same row filtering
	// without each call site re-loading policies from the database.
	// DB-backed policy loading / cache invalidation arrives in a follow-up
	// story; for now the engine boots empty and SetPolicies is exposed for
	// tests and the upcoming loader to populate.
	deps.PolicyEngine = security.NewEngine()
	// US-081: load persisted security policies from the security_policies
	// table so that policies seeded by e2e fixtures (or future admin CRUD)
	// are enforced from the first request. Best-effort: a load failure logs
	// a warning but does not block startup.
	if deps.PGPool != nil {
		if err := loadPoliciesFromDB(ctx, deps.PGPool, deps.PolicyEngine); err != nil {
			log.Printf("[POLICY] warning: failed to load policies from DB: %v", err)
		}
	}
	if impl, ok := deps.OssSvc.(*oss.ServiceImpl); ok && impl != nil {
		impl.SetPolicyEngine(deps.PolicyEngine)
	}

	// 4c. US-256 Row-Level Policy Engine. Lives alongside the existing
	// security.Engine so both enforcement surfaces compose via AND in the
	// OSS service's Load/Search paths. A store failure at boot logs a
	// warning but does not block startup — new policies will start
	// enforcing after the first successful admin write + Reload.
	if deps.RowPolicyStore != nil {
		var gl rls.GroupMembershipLookup
		if deps.GroupRepo != nil {
			gl = newGroupLookupFromRepo(deps.GroupRepo)
		}
		deps.RowPolicyEngine = rls.New(deps.RowPolicyStore, gl)
		if err := deps.RowPolicyEngine.Reload(ctx); err != nil {
			log.Printf("[RLS] warning: failed to load row policies from DB: %v", err)
		}
		if impl, ok := deps.OssSvc.(*oss.ServiceImpl); ok && impl != nil {
			impl.SetRowPolicyEngine(deps.RowPolicyEngine)
		}
	}

	// 4d. US-257 Column-Level Masking Engine. Lives alongside the RLS
	// engine; the masking engine rewrites property values in the returned
	// WireObjects after row-level filters run. A store failure at boot
	// logs a warning but does not block startup — enforcement starts the
	// moment the first Reload succeeds, which the admin handler triggers
	// on every successful write.
	if deps.ColumnMaskStore != nil {
		var gl masking.GroupMembershipLookup
		if deps.GroupRepo != nil {
			gl = newGroupLookupFromRepo(deps.GroupRepo)
		}
		deps.ColumnMaskEngine = masking.New(deps.ColumnMaskStore, gl)
		if err := deps.ColumnMaskEngine.Reload(ctx); err != nil {
			log.Printf("[Masking] warning: failed to load column masks from DB: %v", err)
		}
		if impl, ok := deps.OssSvc.(*oss.ServiceImpl); ok && impl != nil {
			impl.SetColumnMaskEngine(deps.ColumnMaskEngine)
		}
	}

	// 4d2. US-264 Data-Access Audit. The auditor is a thin wrapper over the
	// AuditStore; it emits an audit_events row (action = "data.access") on
	// successful OSS reads of ObjectTypes whose AuditDataAccess flag is
	// true. Wired unconditionally — a nil AuditStore produces a no-op
	// auditor so degraded-mode test routers inherit a safe default.
	if impl, ok := deps.OssSvc.(*oss.ServiceImpl); ok && impl != nil {
		impl.SetDataAccessAuditor(oss.NewDataAccessAuditor(deps.AuditStore))
	}

	// 4e. US-258 Cell-Level Security Engine. Indexes cell_masks by
	// (objectTypeRID, primaryKey) so read paths can look up applicable
	// per-row transforms in O(1). Runs AFTER the column-mask engine so
	// cell-specific rules can sharpen (or add to) the column-wide policy
	// for a single instance. GroupMembershipLookup is shared with
	// masking/rls via Go's structural typing (one adapter serves all).
	if deps.CellMaskStore != nil {
		var gl cellsec.GroupMembershipLookup
		if deps.GroupRepo != nil {
			gl = newGroupLookupFromRepo(deps.GroupRepo)
		}
		deps.CellMaskEngine = cellsec.New(deps.CellMaskStore, gl)
		if err := deps.CellMaskEngine.Reload(ctx); err != nil {
			log.Printf("[CellSec] warning: failed to load cell masks from DB: %v", err)
		}
		if impl, ok := deps.OssSvc.(*oss.ServiceImpl); ok && impl != nil {
			impl.SetCellMaskEngine(deps.CellMaskEngine)
		}
	}

	// 5. Aggregation Engine
	deps.AggEngine = aggregation.NewEngine()

	// 6. ObjectSet
	deps.ObjSetStore = objectset.NewStore(1 * time.Hour)
	if deps.LinkResolver != nil {
		deps.ObjSetExecutor = objectset.NewExecutor(deps.IndexMgr, deps.LinkResolver, deps.ObjSetStore)
		// US-041: wire the PG-backed InterfaceResolver so interfaceBase
		// ObjectSets resolve to their implementing ObjectType apiNames via
		// the object_type_interfaces table. Without this, live
		// loadObjectsOrInterfaces requests for a polymorphic HasOwner-style
		// interface short-circuit with "interface resolver not configured".
		if deps.OmsRepo != nil {
			deps.ObjSetExecutor.SetInterfaceResolver(newPGInterfaceResolver(deps.OmsRepo))
		}
		// US-046: wire the shared row-level policy engine into the
		// executor through a narrow adapter so LoadObjectSet / Aggregate
		// paths enforce the same row filter as Load / Search. US-256
		// layers the row_policies engine onto the same adapter so both
		// security surfaces compose on the ObjectSet path too.
		if deps.OmsRepo != nil && (deps.PolicyEngine != nil || deps.RowPolicyEngine != nil) {
			adapter := newPolicyQueryAdapter(deps.OmsRepo, deps.PolicyEngine)
			if deps.RowPolicyEngine != nil {
				adapter.SetRowPolicyEngine(deps.RowPolicyEngine)
			}
			deps.ObjSetExecutor.SetPolicyProvider(adapter)
		}
		// US-046: nearestNeighbors backend. The vector store wraps the OMS
		// repo (which exposes pgvector via FindNearestNeighbors) and the
		// optional embedding provider resolves text-only NN queries.
		if deps.OmsRepo != nil {
			deps.ObjSetExecutor.SetVectorStore(newPGVectorStore(deps.OmsRepo))
		}
		if prov := buildEmbeddingProvider(); prov != nil {
			deps.ObjSetExecutor.SetEmbeddingProvider(prov)
		}
		// US-210: edge-property enrichment for searchAround over M2M links.
		// Uses the uncached pgRepo (for LinkType lookups) + the LinkEdgeStore
		// to surface link_edges.edge_properties keyed by the "other end" PK.
		if deps.OmsRepo != nil && deps.LinkEdgeStore != nil {
			deps.ObjSetExecutor.SetEdgePropertiesProvider(newPGEdgePropertiesResolver(deps.OmsRepo, deps.LinkEdgeStore))
		}
	}

	// 7b. WebSocket subscription hub (US-132). Created before the NATS
	// consumer so the SetOnChange callback can safely reference it.
	deps.WebSocketHub = subscriptions.NewHub()

	// 7. NATS
	var publisher *funnel.Publisher
	if cfg.NATSURL != "" {
		nc, err := funnel.Connect(cfg.NATSURL)
		if err != nil {
			log.Fatalf("nats connect: %v", err)
		}
		defer nc.Close()
		deps.NATSConn = nc

		js, err := nc.JetStream()
		if err != nil {
			log.Fatalf("jetstream: %v", err)
		}

		if err := funnel.SetupJetStream(js); err != nil {
			log.Printf("warning: jetstream setup: %v", err)
		}

		publisher = funnel.NewPublisher(js)
		deps.FunnelPublisher = publisher
		// US-063: per-ontology ingest rate limiter.
		deps.IngestRateLimiter = oss.NewPerOntologyRateLimiter(
			cfg.IngestRateLimit.RatePerSec,
			cfg.IngestRateLimit.Burst,
		)
		deps.FunnelConsumer = funnel.NewConsumer(js, deps.IndexMgr)
		deps.FunnelConsumer.SetDLQPublish(funnel.NewDLQPublishFunc(js))

		// US-055: stand up the in-process SSE broadcast hub and have the
		// consumer fan every applied edit onto it so HTTP subscribers can
		// tail the change stream. Event.Type is CREATE / MODIFY / DELETE,
		// matching funnel.EditType, which the SSE handler collapses to
		// ADDED_OR_UPDATED / DELETED on the wire.
		deps.FunnelBroadcast = funnel.NewBroadcast()
		deps.FunnelConsumer.SetOnChange(func(e funnel.ChangeEvent) {
			// US-057: carry the NATS stream sequence forward as the
			// BroadcastEvent.Sequence so SSE subscribers can use it as the
			// Server-Sent Events `id:` value and reconnect with a
			// Last-Event-ID header for gap-free replay.
			deps.FunnelBroadcast.Publish(funnel.BroadcastEvent{
				Type:       string(e.EditType),
				ObjectType: e.ObjectType,
				PrimaryKey: e.PrimaryKey,
				Sequence:   e.Offset,
			})

			// US-134: route change events to WebSocket subscribers.
			deps.WebSocketHub.HandleObjectChange(
				e.ObjectType, e.PrimaryKey, string(e.EditType), e.Properties,
			)
		})

		// US-261: marking inheritance via LinkType.PropagateMarkings. The
		// resolver looks up the LinkType + source/target ObjectType API
		// names so the funnel consumer can copy `_markings` from the source
		// onto the target after a successful LINK_CREATE upsert. Wired only
		// when an OMS repo is available; degraded-mode (no PG) routers leave
		// it unset and the consumer silently no-ops.
		if deps.OmsRepo != nil {
			deps.FunnelConsumer.SetLinkPropagationResolver(newLinkPropagationResolver(deps.OmsRepo))
		}

		// US-263: PII auto-detection. The scanner uses pure-Go regexes
		// (email / SSN / phone / Luhn-checked credit card) and is cheap
		// enough to run on every CREATE/MODIFY without a feature flag.
		// A positive match appends the well-known "PII" marking to the
		// edit so the existing marking-based mandatory access control
		// gates visibility on the very first read.
		deps.FunnelConsumer.SetPIIDetector(pii.NewScanner())

		// US-046: optional embedding side-channel. Each CREATE/MODIFY edit
		// for a configured object type produces a vector via the embedding
		// provider, rate-limited to the configured tokens-per-second budget.
		// Disabled when no provider, no store, or no objectTypes are set.
		if prov := buildEmbeddingProvider(); prov != nil && deps.OmsRepo != nil {
			deps.FunnelConsumer.SetEmbeddingProvider(prov)
			deps.FunnelConsumer.SetEmbeddingStore(deps.OmsRepo)
			if cfgMap := loadEmbeddingObjectTypes(); len(cfgMap) > 0 {
				deps.FunnelConsumer.SetEmbeddingObjectTypes(cfgMap)
				deps.FunnelConsumer.SetEmbeddingObjectTypeRIDs(loadObjectTypeRIDs(ctx, deps.OmsRepo))
			}
			if lim := buildEmbeddingRateLimiter(); lim != nil {
				deps.FunnelConsumer.SetEmbeddingRateLimiter(lim)
			}
		}

		if err := deps.FunnelConsumer.Start(ctx); err != nil {
			log.Printf("warning: funnel consumer start: %v", err)
		}
	}

	// 8. Action Executor
	if deps.OmsRepo != nil {
		deps.ActionExecutor = actions.NewExecutor(deps.OmsRepo, publisher)
		// US-238: opt-in PG-transactional batch commit via ?atomic=true.
		// Wired only when a PG pool exists — the store lives on the
		// uncached *PGRepository (same pattern as LinkPropertyStore /
		// MediaCatalog / ObjectSetSnapshotStore).
		if deps.PGPool != nil {
			deps.ActionExecutor.SetAtomicActionLogStore(oms.NewPGRepository(deps.PGPool))
			// US-240: async apply persists job state to action_jobs. The PG
			// adapter lives in cmd/server/ so pkg/oms stays free of a
			// pkg/actions import (actions already imports oms). In degraded
			// mode (no PG) the handler silently falls back to sync Apply —
			// see serveAsyncApply in pkg/actions/handlers.go.
			deps.ActionExecutor.SetActionJobStore(newPGActionJobStore(deps.PGPool))
			// US-242: approval-workflow store persists pending / terminal
			// approval rows. Wired in the same PG-gated block as the job
			// store — degraded-mode (no PG) routers keep the sync-apply
			// contract stable for unit tests.
			deps.ActionExecutor.SetActionApprovalStore(newPGActionApprovalStore(deps.PGPool))
		}
		// US-241: progress reporter publishes to NATS on actions.progress.<jobId>
		// when the JS action calls weave.reportProgress(percent, message). The
		// store update still runs without NATS (degraded mode), but live fanout
		// requires a NATS connection. Publisher is setter-injected so handler
		// tests without NATS / PG keep working — mirrors the AtomicActionLogStore
		// / ActionJobStore wiring pattern.
		if deps.NATSConn != nil {
			deps.ActionExecutor.SetProgressPublisher(newNATSActionProgressPublisher(deps.NATSConn))
		}
		// US-214: polymorphic invoke forwards to the ActionExecutor via a
		// narrow adapter so pkg/oms stays free of a pkg/actions import.
		deps.InterfaceMethodDispatcher = newInterfaceMethodDispatcher(deps.ActionExecutor)
	}

	// Build router
	router := NewFullRouter(deps)

	// Mount WebUI SPA handler
	dist, err := webDistFS()
	if err != nil {
		log.Printf("warning: webui not available: %v", err)
	} else {
		router.NotFound(spaHandler(dist))
		log.Println("webui enabled at /")
	}

	// Graceful shutdown
	srv := NewServer(router, cfg.Port)

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		// US-132: close WebSocket hub before stopping HTTP server
		if deps.WebSocketHub != nil {
			deps.WebSocketHub.Close()
		}
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := gracefulShutdown(shutdownCtx, srv, deps.FunnelConsumer); err != nil {
			log.Printf("graceful shutdown error: %v", err)
		}
	}()

	log.Printf("starting Weave server on :%d", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
