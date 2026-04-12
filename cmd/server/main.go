package main

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liyang/weave/internal/config"
	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/attachment"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/cipher"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/geotemporal"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/mcp"
	"github.com/liyang/weave/pkg/metrics"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/oss/objectset"
	"github.com/liyang/weave/pkg/security"
	"github.com/liyang/weave/pkg/sqlqueries"
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
	FunnelBroadcast   *funnel.Broadcast
	AttachmentStore   attachment.BlobStore
	TimeSeriesStore   timeseries.Store
	GeotemporalStore  geotemporal.Store
	CipherDecryptor   cipher.Decryptor
	TransactionStore  transactions.Store
	SqlQueryEngine    sqlqueries.Engine
	IndexDocSource    index.LatestDocumentSource // Authoritative source for index.Rebuild (nil in degraded mode)
	AuditStore        audit.Store                 // US-067: audit event store (nil = endpoint returns 503)
	IngestRateLimiter oss.IngestRateLimiter      // US-063: per-ontology token-bucket (nil = no limit)
	CORSOrigins       []string                   // Allowed CORS origins (empty = disabled)
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
		loginHandler := auth.NewLoginHandler(auth.LoginHandlerDeps{
			Users:          deps.UserRepo,
			Resolver:       deps.RoleResolver,
			Signer:         deps.JWTSigner,
			RefreshService: deps.RefreshService,
			RateLimit:      10,
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

	// Auth-protected API routes
	r.Group(func(api chi.Router) {
		// MiddlewareWithAPIKeys unifies JWT and wvk_ api-key bearer auth.
		// When any of the api-key dependencies is nil (dev / minimal test
		// harness) it degrades gracefully to JWT-only — byte-identical to
		// the old auth.Middleware(signer) behaviour because Middleware is
		// a thin wrapper over MiddlewareWithAPIKeys(signer, nil, nil, nil).
		api.Use(auth.MiddlewareWithAPIKeys(
			deps.JWTSigner,
			deps.APIKeyRepo,
			deps.UserRepo,
			deps.RoleResolver,
		))

		// US-044: enforce per-ontology scope on every route that carries an
		// {ontologyApiName} URL param. Dev mode injects an admin user so this
		// middleware is a no-op for the existing dev surface; in jwt mode it
		// rejects requests where the caller has no role for the target
		// ontology. Routes without an {ontologyApiName} param (auth/me, sql
		// queries, attachments) skip the check because there is nothing to
		// scope to.
		api.Use(auth.OntologyScopeMiddleware(auth.PermObjectRead))

		// Current-user endpoint (RBAC Phase 1)
		api.Method(http.MethodGet, "/api/v2/me", auth.MeHandler())

		// OMS routes
		if deps.OmsRepo != nil {
			omsHandler := oms.NewOMSHandler(deps.OmsRepo)
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
			api.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", objSetHandler.LoadObjects)
			api.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadLinks", objSetHandler.LoadLinks)
			api.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/aggregate", objSetHandler.Aggregate)
			api.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/createTemporary", objSetHandler.CreateTemporary)
			// Foundry preview endpoints: multi-type + interface-scoped loads.
			// Register these BEFORE the wildcard /{objectSetRid} so chi does
			// not swallow the static path segments.
			api.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsMultipleObjectTypes", objSetHandler.LoadObjectsMultipleObjectTypes)
			api.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsOrInterfaces", objSetHandler.LoadObjectsOrInterfaces)
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
		// US-067: PG-backed audit event store for the admin read endpoint.
		deps.AuditStore = audit.NewPGStore(pool)
		// US-011: the index rebuild admin command re-ingests from
		// object_history. Keep the uncached *PGRepository reference so the
		// rebuild path always observes the authoritative tail.
		deps.IndexDocSource = newPGIndexDocSource(pgRepo)
		deps.UserRepo = auth.NewPGUserRepository(pool)
		deps.APIKeyRepo = auth.NewPGAPIKeyRepository(pool)
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
	}

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
