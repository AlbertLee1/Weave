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

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liyang/weave/internal/config"
	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/attachment"
	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/cipher"
	"github.com/liyang/weave/pkg/developer"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/geotemporal"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/mcp"
	"github.com/liyang/weave/pkg/media"
	"github.com/liyang/weave/pkg/metrics"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/oss/objectset"
	"github.com/liyang/weave/pkg/security"
	"github.com/liyang/weave/pkg/sqlqueries"
	"github.com/liyang/weave/pkg/subscriptions"
	"github.com/liyang/weave/pkg/timeseries"
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
	TimeSeriesStore      timeseries.Store
	GeotemporalStore     geotemporal.Store
	CipherDecryptor      cipher.Decryptor
	TransactionStore     transactions.Store
	SqlQueryEngine       sqlqueries.Engine
	IndexDocSource       index.LatestDocumentSource // Authoritative source for index.Rebuild (nil in degraded mode)
	AuditStore           audit.Store                // US-067: audit event store (nil = endpoint returns 503)
	IngestRateLimiter    oss.IngestRateLimiter      // US-063: per-ontology token-bucket (nil = no limit)
	WebSocketHub         *subscriptions.Hub         // US-132: WebSocket subscription hub (nil = endpoint not mounted)
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
	CORSOrigins    []string // Allowed CORS origins (empty = disabled)
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
	if deps.CORSOrigins != nil && len(deps.CORSOrigins) > 0 {
		r.Use(CORSMiddleware(deps.CORSOrigins))
	}
	// US-069: per-endpoint rate limiting with default fallback.
	rateLimitRules, defaultRateLimitRule := DefaultRateLimitRules()
	r.Use(NewRateLimitMiddlewareWithDefault(rateLimitRules, defaultRateLimitRule))

	// Health endpoints (public, no auth required)
	// /health is the k8s liveness probe: always returns 200 {"status":"alive"}
	// /health/ready is the k8s readiness probe: checks PG/NATS/Bleve, 503 if
	// any configured dependency is unhealthy.
	r.Method(http.MethodGet, "/health", LivenessHandler())
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
		loginHandler := auth.NewLoginHandler(auth.LoginHandlerDeps{
			Users:          deps.UserRepo,
			Resolver:       deps.RoleResolver,
			Signer:         deps.JWTSigner,
			RefreshService: deps.RefreshService,
			RateLimit:      loginRateLimit,
			MarkingRepo:    markingRepo,
		})
		refreshHandler := auth.NewRefreshHandler(auth.RefreshHandlerDeps{
			Users:          deps.UserRepo,
			Resolver:       deps.RoleResolver,
			Signer:         deps.JWTSigner,
			RefreshService: deps.RefreshService,
		})
		logoutHandler := auth.NewLogoutHandler(deps.RefreshService, nil)
		r.Method(http.MethodPost, "/api/auth/login", loginHandler)
		r.Method(http.MethodPost, "/api/auth/refresh", refreshHandler)
		r.Method(http.MethodPost, "/api/auth/logout", logoutHandler)
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

	deps := &ServerDeps{
		CORSOrigins: cfg.CORSOrigins,
	}

	// 1. PostgreSQL
	if cfg.PGDSN != "" {
		pool, err := database.Connect(ctx, cfg.PGDSN)
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
		deps.AuditStore = audit.NewPGStore(pool)
		// US-011: the index rebuild admin command re-ingests from
		// object_history. Keep the uncached *PGRepository reference so the
		// rebuild path always observes the authoritative tail.
		deps.IndexDocSource = newPGIndexDocSource(pgRepo)
		deps.UserRepo = auth.NewPGUserRepository(pool)
		deps.APIKeyRepo = auth.NewPGAPIKeyRepository(pool)
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
		// paths enforce the same row filter as Load / Search.
		if deps.OmsRepo != nil && deps.PolicyEngine != nil {
			deps.ObjSetExecutor.SetPolicyProvider(newPolicyQueryAdapter(deps.OmsRepo, deps.PolicyEngine))
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
