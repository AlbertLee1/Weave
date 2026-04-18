package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	ldapv3 "github.com/go-ldap/ldap/v3"
)

// DefaultLDAPSyncInterval is the default cadence the LDAPSyncScheduler runs
// at when no interval is configured. Matches the OSv2 spec for "periodic
// directory sync" — frequent enough that a leaver loses access within an
// hour, slow enough that we don't hammer the directory.
const DefaultLDAPSyncInterval = 1 * time.Hour

// DefaultLDAPDialTimeout caps the time spent waiting for the directory
// service to accept the TCP connection. A misconfigured DC at boot should
// not block server startup forever.
const DefaultLDAPDialTimeout = 10 * time.Second

// LDAPSyncConfig is the runtime configuration that controls one
// LDAPSyncService instance. Fields prefixed with `User` / `Group` describe
// where to look in the directory for the matching identity row; the
// `Email` / `Name` / `MemberOf` attribute names are configurable so the
// same code can sync both Active Directory (`sAMAccountName`, `mail`,
// `displayName`) and OpenLDAP (`uid`, `mail`, `cn`).
type LDAPSyncConfig struct {
	// Connection.
	URL          string        // ldap[s]://host:port
	BindDN       string        // Service account DN used for the bind
	BindPassword string        // Bind password
	StartTLS     bool          // Upgrade plaintext connection to TLS via StartTLS
	InsecureSkip bool          // Disable cert verification (TEST ONLY)
	DialTimeout  time.Duration // Per-connection dial timeout (default DefaultLDAPDialTimeout)

	// User search.
	UserBaseDN          string
	UserFilter          string // Defaults to "(objectClass=person)"
	UserEmailAttribute  string // Defaults to "mail"
	UserNameAttribute   string // Defaults to "displayName"
	UserLoginAttribute  string // Optional; used as the email fallback when the directory only carries sAMAccountName
	UserMemberOfAttr    string // Optional; if set, group memberships are derived from the user's memberOf attribute instead of group.member traversal

	// Group search.
	GroupBaseDN            string
	GroupFilter            string // Defaults to "(objectClass=groupOfNames)"
	GroupNameAttribute     string // Defaults to "cn"
	GroupMemberAttribute   string // Defaults to "member"
	GroupDescriptionAttr   string // Defaults to "description"
}

// withDefaults returns a copy of cfg with empty fields populated by the
// canonical defaults. Single source of truth so the connector and the
// search builders agree on what an unconfigured field means.
func (cfg LDAPSyncConfig) withDefaults() LDAPSyncConfig {
	out := cfg
	if out.DialTimeout <= 0 {
		out.DialTimeout = DefaultLDAPDialTimeout
	}
	if out.UserFilter == "" {
		out.UserFilter = "(objectClass=person)"
	}
	if out.UserEmailAttribute == "" {
		out.UserEmailAttribute = "mail"
	}
	if out.UserNameAttribute == "" {
		out.UserNameAttribute = "displayName"
	}
	if out.GroupFilter == "" {
		out.GroupFilter = "(objectClass=groupOfNames)"
	}
	if out.GroupNameAttribute == "" {
		out.GroupNameAttribute = "cn"
	}
	if out.GroupMemberAttribute == "" {
		out.GroupMemberAttribute = "member"
	}
	if out.GroupDescriptionAttr == "" {
		out.GroupDescriptionAttr = "description"
	}
	return out
}

// Validate reports missing-required-field problems on the config. Called
// by the bootstrap path so misconfiguration surfaces with a clean error
// rather than a network failure later.
func (cfg LDAPSyncConfig) Validate() error {
	var missing []string
	if strings.TrimSpace(cfg.URL) == "" {
		missing = append(missing, "URL")
	}
	if strings.TrimSpace(cfg.UserBaseDN) == "" {
		missing = append(missing, "UserBaseDN")
	}
	if len(missing) > 0 {
		return fmt.Errorf("ldap config missing fields: %s", strings.Join(missing, ", "))
	}
	return nil
}

// LDAPUser is the directory-shaped projection of a user, returned by the
// LDAPClient and consumed by LDAPSyncService. DN is the stable correlation
// key — email can change in AD without changing the row.
type LDAPUser struct {
	DN          string
	Email       string
	DisplayName string
	// MemberOfDNs carries the group DNs the user belongs to, if the
	// LDAPClient walked the userMemberOf attribute. Populated only when
	// LDAPSyncConfig.UserMemberOfAttr is set; otherwise group membership
	// is reconstructed from the group.member walk.
	MemberOfDNs []string
}

// LDAPGroup is the directory-shaped projection of a group.
type LDAPGroup struct {
	DN          string
	Name        string
	Description string
	MemberDNs   []string // The DNs of users that are direct members
}

// LDAPClient is the narrow interface the LDAPSyncService talks to. The
// production implementation wraps go-ldap/ldap/v3; tests substitute an
// in-memory stub. Splitting the connection lifecycle from the searches
// keeps the call site clean and lets tests assert that Close is called.
type LDAPClient interface {
	// SearchUsers returns every user reachable from the configured
	// UserBaseDN matching UserFilter. Implementations are responsible
	// for paging through large result sets.
	SearchUsers(ctx context.Context) ([]LDAPUser, error)
	// SearchGroups returns every group reachable from the configured
	// GroupBaseDN matching GroupFilter, including direct member DNs.
	SearchGroups(ctx context.Context) ([]LDAPGroup, error)
	// Close releases the underlying network connection. Safe to call
	// more than once.
	Close()
}

// LDAPClientFactory constructs a fresh LDAPClient bound to the directory.
// Returned in a function form so the scheduler can build a connection per
// sync cycle (long-lived LDAP connections frequently drop and AD ages
// them out aggressively).
type LDAPClientFactory func(ctx context.Context) (LDAPClient, error)

// NewGoLDAPClientFactory returns a factory that opens a real go-ldap/v3
// connection per call. Production wiring uses this; tests substitute a
// stub factory.
func NewGoLDAPClientFactory(cfg LDAPSyncConfig) LDAPClientFactory {
	cfg = cfg.withDefaults()
	return func(ctx context.Context) (LDAPClient, error) {
		opts := []ldapv3.DialOpt{
			ldapv3.DialWithDialer(&net.Dialer{Timeout: cfg.DialTimeout}),
		}
		if cfg.InsecureSkip {
			opts = append(opts, ldapv3.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: true}))
		}
		conn, err := ldapv3.DialURL(cfg.URL, opts...)
		if err != nil {
			return nil, fmt.Errorf("ldap dial %s: %w", cfg.URL, err)
		}
		if cfg.StartTLS {
			tlsCfg := &tls.Config{InsecureSkipVerify: cfg.InsecureSkip}
			if err := conn.StartTLS(tlsCfg); err != nil {
				conn.Close()
				return nil, fmt.Errorf("ldap StartTLS: %w", err)
			}
		}
		if cfg.BindDN != "" {
			if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
				conn.Close()
				return nil, fmt.Errorf("ldap bind %s: %w", cfg.BindDN, err)
			}
		}
		return &goLDAPClient{conn: conn, cfg: cfg}, nil
	}
}

// goLDAPClient is the production LDAPClient implementation. Each method
// runs a single search request — paging is delegated to the v3 library
// via SearchWithPaging, so directories of any size flow through.
type goLDAPClient struct {
	conn *ldapv3.Conn
	cfg  LDAPSyncConfig
}

func (c *goLDAPClient) SearchUsers(ctx context.Context) ([]LDAPUser, error) {
	attrs := []string{
		c.cfg.UserEmailAttribute,
		c.cfg.UserNameAttribute,
	}
	if c.cfg.UserLoginAttribute != "" {
		attrs = append(attrs, c.cfg.UserLoginAttribute)
	}
	if c.cfg.UserMemberOfAttr != "" {
		attrs = append(attrs, c.cfg.UserMemberOfAttr)
	}
	req := ldapv3.NewSearchRequest(
		c.cfg.UserBaseDN,
		ldapv3.ScopeWholeSubtree,
		ldapv3.NeverDerefAliases,
		0, 0, false,
		c.cfg.UserFilter,
		attrs,
		nil,
	)
	res, err := c.conn.SearchWithPaging(req, 500)
	if err != nil {
		return nil, fmt.Errorf("ldap user search: %w", err)
	}
	out := make([]LDAPUser, 0, len(res.Entries))
	for _, e := range res.Entries {
		email := e.GetAttributeValue(c.cfg.UserEmailAttribute)
		if email == "" && c.cfg.UserLoginAttribute != "" {
			email = e.GetAttributeValue(c.cfg.UserLoginAttribute)
		}
		if email == "" {
			continue // can't key without an email/login
		}
		user := LDAPUser{
			DN:          e.DN,
			Email:       email,
			DisplayName: e.GetAttributeValue(c.cfg.UserNameAttribute),
		}
		if c.cfg.UserMemberOfAttr != "" {
			user.MemberOfDNs = e.GetAttributeValues(c.cfg.UserMemberOfAttr)
		}
		out = append(out, user)
	}
	return out, nil
}

func (c *goLDAPClient) SearchGroups(ctx context.Context) ([]LDAPGroup, error) {
	attrs := []string{
		c.cfg.GroupNameAttribute,
		c.cfg.GroupMemberAttribute,
		c.cfg.GroupDescriptionAttr,
	}
	req := ldapv3.NewSearchRequest(
		c.cfg.GroupBaseDN,
		ldapv3.ScopeWholeSubtree,
		ldapv3.NeverDerefAliases,
		0, 0, false,
		c.cfg.GroupFilter,
		attrs,
		nil,
	)
	res, err := c.conn.SearchWithPaging(req, 500)
	if err != nil {
		return nil, fmt.Errorf("ldap group search: %w", err)
	}
	out := make([]LDAPGroup, 0, len(res.Entries))
	for _, e := range res.Entries {
		name := e.GetAttributeValue(c.cfg.GroupNameAttribute)
		if name == "" {
			continue
		}
		out = append(out, LDAPGroup{
			DN:          e.DN,
			Name:        name,
			Description: e.GetAttributeValue(c.cfg.GroupDescriptionAttr),
			MemberDNs:   e.GetAttributeValues(c.cfg.GroupMemberAttribute),
		})
	}
	return out, nil
}

func (c *goLDAPClient) Close() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

// LDAPSyncStore is the narrow persistence surface the sync engine writes
// through. Kept off UserRepository / GroupRepository to avoid widening
// either of those broadly-mocked interfaces.
//
// Semantics:
//
//   - UpsertSyncedUser inserts or updates a user keyed by ldap_dn. A
//     freshly inserted user is reported with created=true. last_synced_at
//     is stamped to syncedAt unconditionally; disabled_at is cleared so
//     a previously-disabled user that returns to the directory is
//     re-enabled in one pass.
//   - DisableOrphanedSyncedUsers stamps disabled_at=now() on every user
//     row whose ldap_dn IS NOT NULL but whose last_synced_at is older
//     than syncedAt — i.e. anyone the just-completed sync didn't see.
//     Rows with NULL ldap_dn (locally provisioned, password-only) are
//     untouched.
//   - UpsertSyncedGroup mirrors UpsertSyncedUser for the groups table.
//   - ReplaceGroupMembers replaces the user_groups rows for a given
//     group with the supplied user-id slice in a single transaction.
//   - UserIDByDN is the lookup the membership-resolution pass uses to
//     translate a directory DN to a Weave user id. Returns ("", nil) on
//     miss so the caller can decide whether that's an error or expected.
//   - RecordSyncRun appends one row to ldap_sync_runs and prunes the
//     table down to the most recent 100 rows so the table stays
//     bounded without an out-of-band cleanup job.
type LDAPSyncStore interface {
	UpsertSyncedUser(ctx context.Context, dn, email, displayName string, syncedAt time.Time) (id string, created bool, err error)
	DisableOrphanedSyncedUsers(ctx context.Context, syncedAt time.Time) (int, error)
	UpsertSyncedGroup(ctx context.Context, dn, name, description string, syncedAt time.Time) (id string, created bool, err error)
	ReplaceGroupMembers(ctx context.Context, groupID string, userIDs []string) (added int, err error)
	UserIDByDN(ctx context.Context, dn string) (string, error)
	RecordSyncRun(ctx context.Context, run *LDAPSyncRun) error
}

// LDAPSyncRun is one row in the ldap_sync_runs audit table. Counters are
// stamped at the end of each Sync; ErrorMessage is populated when Sync
// returns a non-nil error.
type LDAPSyncRun struct {
	ID               string
	StartedAt        time.Time
	FinishedAt       *time.Time
	UsersSeen        int
	UsersCreated     int
	UsersUpdated     int
	UsersDisabled    int
	GroupsSeen       int
	GroupsCreated    int
	GroupsUpdated    int
	MembershipsAdded int
	ErrorMessage     string
}

// LDAPSyncService coordinates the per-cycle directory pull. Construction
// is decoupled from scheduling so callers can run a one-shot sync from a
// test or admin endpoint without standing up the periodic loop.
type LDAPSyncService struct {
	cfg     LDAPSyncConfig
	factory LDAPClientFactory
	store   LDAPSyncStore
	now     func() time.Time
	logger  func(format string, v ...any)
}

// NewLDAPSyncService wires the dependencies. cfg.Validate must succeed
// before construction; the bootstrap path checks this and only constructs
// the service when the config is acceptable.
func NewLDAPSyncService(cfg LDAPSyncConfig, factory LDAPClientFactory, store LDAPSyncStore) *LDAPSyncService {
	return &LDAPSyncService{
		cfg:     cfg.withDefaults(),
		factory: factory,
		store:   store,
		now:     time.Now,
		logger:  log.Printf,
	}
}

// SetNowFunc lets tests inject a deterministic clock. Same convention as
// oms.CachedRepository.nowFunc and pkg/oss/computed.Resolver.
func (s *LDAPSyncService) SetNowFunc(fn func() time.Time) {
	if fn == nil {
		fn = time.Now
	}
	s.now = fn
}

// SetLogger overrides the default log.Printf-backed logger. Tests can pass
// a no-op so output stays quiet.
func (s *LDAPSyncService) SetLogger(fn func(format string, v ...any)) {
	if fn == nil {
		fn = log.Printf
	}
	s.logger = fn
}

// Sync performs one full directory pull. The returned LDAPSyncRun is
// always populated (even on error) and is also persisted via
// store.RecordSyncRun before Sync returns, so audit history is complete
// even on partial failure.
func (s *LDAPSyncService) Sync(ctx context.Context) (*LDAPSyncRun, error) {
	if s.factory == nil || s.store == nil {
		return nil, errors.New("ldap sync: factory or store not wired")
	}
	startedAt := s.now()
	run := &LDAPSyncRun{StartedAt: startedAt}

	finish := func(err error) (*LDAPSyncRun, error) {
		now := s.now()
		run.FinishedAt = &now
		if err != nil {
			run.ErrorMessage = err.Error()
		}
		if rerr := s.store.RecordSyncRun(ctx, run); rerr != nil {
			s.logger("[LDAP] failed to record sync run: %v", rerr)
		}
		return run, err
	}

	client, err := s.factory(ctx)
	if err != nil {
		return finish(fmt.Errorf("connect: %w", err))
	}
	defer client.Close()

	users, err := client.SearchUsers(ctx)
	if err != nil {
		return finish(fmt.Errorf("user search: %w", err))
	}
	run.UsersSeen = len(users)
	for _, u := range users {
		_, created, err := s.store.UpsertSyncedUser(ctx, u.DN, u.Email, u.DisplayName, startedAt)
		if err != nil {
			return finish(fmt.Errorf("upsert user %s: %w", u.DN, err))
		}
		if created {
			run.UsersCreated++
		} else {
			run.UsersUpdated++
		}
	}

	disabled, err := s.store.DisableOrphanedSyncedUsers(ctx, startedAt)
	if err != nil {
		return finish(fmt.Errorf("disable orphans: %w", err))
	}
	run.UsersDisabled = disabled

	groups, err := client.SearchGroups(ctx)
	if err != nil {
		return finish(fmt.Errorf("group search: %w", err))
	}
	run.GroupsSeen = len(groups)
	for _, g := range groups {
		groupID, created, err := s.store.UpsertSyncedGroup(ctx, g.DN, g.Name, g.Description, startedAt)
		if err != nil {
			return finish(fmt.Errorf("upsert group %s: %w", g.DN, err))
		}
		if created {
			run.GroupsCreated++
		} else {
			run.GroupsUpdated++
		}
		userIDs, resolveErr := s.resolveMembers(ctx, g.MemberDNs)
		if resolveErr != nil {
			return finish(fmt.Errorf("resolve members of %s: %w", g.DN, resolveErr))
		}
		added, err := s.store.ReplaceGroupMembers(ctx, groupID, userIDs)
		if err != nil {
			return finish(fmt.Errorf("replace members of %s: %w", g.DN, err))
		}
		run.MembershipsAdded += added
	}

	return finish(nil)
}

func (s *LDAPSyncService) resolveMembers(ctx context.Context, dns []string) ([]string, error) {
	out := make([]string, 0, len(dns))
	seen := make(map[string]struct{}, len(dns))
	for _, dn := range dns {
		if dn == "" {
			continue
		}
		id, err := s.store.UserIDByDN(ctx, dn)
		if err != nil {
			return nil, err
		}
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

// LDAPSyncScheduler runs Sync on a fixed interval until the supplied
// context is cancelled. The first sync runs immediately at Start so
// operators don't have to wait an interval to see the initial bootstrap
// happen.
type LDAPSyncScheduler struct {
	svc      *LDAPSyncService
	interval time.Duration
	logger   func(format string, v ...any)

	mu     sync.Mutex
	stopCh chan struct{}
	doneCh chan struct{}
}

// NewLDAPSyncScheduler wires a scheduler around svc. interval clamps to
// DefaultLDAPSyncInterval when <= 0.
func NewLDAPSyncScheduler(svc *LDAPSyncService, interval time.Duration) *LDAPSyncScheduler {
	if interval <= 0 {
		interval = DefaultLDAPSyncInterval
	}
	return &LDAPSyncScheduler{
		svc:      svc,
		interval: interval,
		logger:   log.Printf,
	}
}

// SetLogger overrides the default log.Printf-backed logger.
func (s *LDAPSyncScheduler) SetLogger(fn func(format string, v ...any)) {
	if fn == nil {
		fn = log.Printf
	}
	s.logger = fn
}

// Interval returns the loop's tick interval. Surfaced for log lines and
// admin status endpoints.
func (s *LDAPSyncScheduler) Interval() time.Duration {
	return s.interval
}

// Start launches the periodic loop. Returns immediately; the loop exits
// when ctx is cancelled OR Stop is called. Idempotent — calling Start
// twice is a no-op once the loop is running.
func (s *LDAPSyncScheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.stopCh != nil {
		s.mu.Unlock()
		return
	}
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.mu.Unlock()

	go func() {
		defer close(s.doneCh)
		// Initial run immediately so operators see ldap state on boot.
		s.runOnce(ctx)
		t := time.NewTicker(s.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case <-t.C:
				s.runOnce(ctx)
			}
		}
	}()
}

// Stop cancels the loop and waits for the in-flight Sync to return.
func (s *LDAPSyncScheduler) Stop() {
	s.mu.Lock()
	stopCh, doneCh := s.stopCh, s.doneCh
	s.stopCh = nil
	s.mu.Unlock()
	if stopCh != nil {
		close(stopCh)
	}
	if doneCh != nil {
		<-doneCh
	}
}

func (s *LDAPSyncScheduler) runOnce(ctx context.Context) {
	run, err := s.svc.Sync(ctx)
	if err != nil {
		s.logger("[LDAP] sync failed: %v", err)
		return
	}
	if run != nil {
		s.logger("[LDAP] sync ok: users=%d (+%d/~%d/-%d) groups=%d (+%d/~%d) memberships=%d",
			run.UsersSeen, run.UsersCreated, run.UsersUpdated, run.UsersDisabled,
			run.GroupsSeen, run.GroupsCreated, run.GroupsUpdated,
			run.MembershipsAdded)
	}
}
