// Package seed_northwind provides a library + CLI that loads a minimal,
// deterministic Northwind ontology into a running Weave installation for
// Playwright E2E tests (US-030). It is deliberately independent of the
// Admin HTTP API (which was removed in v1 US-006): all writes go through
// the oms.PGRepository + auth.PGUserRepository backends directly, keeping
// the seed script fast (<30s) and self-contained.
//
// Seed() is wipe-and-reseed: every call deletes any prior northwind
// ontology state (and the baseline test users) before recreating it, so
// running the script twice produces byte-identical final state. The CLI
// wrapper in main.go additionally calls POST /api/admin/indexes/rebuild
// for each seeded object type, replaying the freshly-written
// object_history rows into Bleve.
package seed_northwind

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
)

// Options controls a Seed() invocation. DefaultOptions() returns the
// baseline Playwright setup; callers may override any field.
type Options struct {
	OntologyAPIName string
	TestUsers       []TestUser
	Logger          *log.Logger
}

// TestUser is one of the baseline user rows created by Seed(). Each
// password is hashed with bcrypt before being written so the Playwright
// login flow can exchange it for a JWT.
type TestUser struct {
	Email    string
	Password string
	Roles    []string
	Markings []string // US-082: marking grants inserted into user_markings
}

// Result summarises what Seed() wrote. It is the wire contract between
// the library and the CLI wrapper — main.go uses ObjectTypes to know
// which indexes to rebuild after the PG writes commit.
type Result struct {
	OntologyAPIName string
	OntologyRID     string
	ObjectTypes     []string
	UserIDs         []string
}

// DefaultOptions returns the baseline Playwright seed configuration:
// ontology apiName "northwind" + admin/manager/peer test users with a
// shared default password.
func DefaultOptions() Options {
	return Options{
		OntologyAPIName: "northwind",
		TestUsers: []TestUser{
			{Email: "admin@test", Password: "test1234", Roles: []string{auth.RoleAdmin}},
			{Email: "manager@test", Password: "test1234", Roles: []string{auth.RoleEditor}},
			{Email: "peer@test", Password: "test1234", Roles: []string{auth.RoleViewer}},
			// US-082: marking-scoped users for policy-row-filter Playwright spec.
			{Email: "acme@test", Password: "test1234", Roles: []string{auth.RoleViewer}, Markings: []string{"ACME"}},
			{Email: "acme2@test", Password: "test1234", Roles: []string{auth.RoleViewer}, Markings: []string{"ACME2"}},
		},
	}
}

// Seed wipes any existing northwind ontology + baseline test users from
// the target Postgres instance and recreates them with a minimal, fixed
// dataset. It is safe to call repeatedly; two consecutive calls converge
// to the same final state.
func Seed(ctx context.Context, pool *pgxpool.Pool, opts Options) (*Result, error) {
	if pool == nil {
		return nil, errors.New("seed: nil postgres pool")
	}
	if opts.OntologyAPIName == "" {
		opts.OntologyAPIName = "northwind"
	}
	logf := func(format string, args ...interface{}) {
		if opts.Logger != nil {
			opts.Logger.Printf(format, args...)
		}
	}

	repo := oms.NewPGRepository(pool)

	// 1. Wipe — every subsequent step assumes a blank slate for the
	//    northwind ontology + the baseline users. Deletes are ordered
	//    from leaf tables back toward the root so foreign key
	//    constraints hold without cascade loops.
	logf("[seed] wiping prior northwind state")
	if err := wipe(ctx, pool, opts); err != nil {
		return nil, fmt.Errorf("seed: wipe: %w", err)
	}

	// 2. Ontology -------------------------------------------------------
	ontRID := stableRID("ontology", opts.OntologyAPIName)
	ont := &oms.Ontology{
		RID:         ontRID,
		APIName:     opts.OntologyAPIName,
		DisplayName: "Northwind Traders",
		Description: "E2E seed ontology for Playwright tests (US-030).",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		return nil, fmt.Errorf("seed: create ontology: %w", err)
	}
	logf("[seed] created ontology %s", ontRID)

	// 3. Object types + properties --------------------------------------
	schemas := northwindSchemas()
	for _, s := range schemas {
		otRID := stableRID("object-type", opts.OntologyAPIName+"-"+s.APIName)
		ot := &oms.ObjectType{
			RID:               otRID,
			OntologyRID:       ontRID,
			APIName:           s.APIName,
			DisplayName:       s.DisplayName,
			PluralDisplayName: s.PluralDisplayName,
			PrimaryKey:        s.PrimaryKey,
			TitleProperty:     s.TitleProperty,
			Status:            "ACTIVE",
			Visibility:        "NORMAL",
		}
		if err := repo.CreateObjectType(ctx, ot); err != nil {
			return nil, fmt.Errorf("seed: create object type %q: %w", s.APIName, err)
		}
		for _, p := range s.Properties {
			prop := &oms.Property{
				RID:           stableRID("property", opts.OntologyAPIName+"-"+s.APIName+"-"+p.APIName),
				ObjectTypeRID: otRID,
				APIName:       p.APIName,
				DisplayName:   p.DisplayName,
				BaseType:      p.BaseType,
				IsSearchable:  p.IsSearchable,
				IsSortable:    p.IsSortable,
			}
			if p.Analyzer != "" {
				cfg, err := json.Marshal(struct {
					Analyzer string `json:"analyzer"`
				}{Analyzer: p.Analyzer})
				if err != nil {
					return nil, fmt.Errorf("seed: marshal analyzer hint for %q.%q: %w", s.APIName, p.APIName, err)
				}
				prop.TypeConfig = cfg
			}
			if err := repo.CreateProperty(ctx, prop); err != nil {
				return nil, fmt.Errorf("seed: create property %q.%q: %w", s.APIName, p.APIName, err)
			}
		}
		logf("[seed] created object type %s with %d properties", s.APIName, len(s.Properties))
	}

	// 4. Link types -----------------------------------------------------
	// Resolve object type RIDs once — link types store them as
	// source_object_type / target_object_type.
	otRIDByName := map[string]string{}
	for _, s := range schemas {
		otRIDByName[s.APIName] = stableRID("object-type", opts.OntologyAPIName+"-"+s.APIName)
	}
	for _, l := range northwindLinkTypes() {
		src, ok := otRIDByName[l.Source]
		if !ok {
			return nil, fmt.Errorf("seed: link type %q references unknown source %q", l.APIName, l.Source)
		}
		tgt, ok := otRIDByName[l.Target]
		if !ok {
			return nil, fmt.Errorf("seed: link type %q references unknown target %q", l.APIName, l.Target)
		}
		lt := &oms.LinkType{
			RID:              stableRID("link-type", opts.OntologyAPIName+"-"+l.APIName),
			OntologyRID:      ontRID,
			APIName:          l.APIName,
			DisplayName:      l.DisplayName,
			SourceObjectType: src,
			TargetObjectType: tgt,
			Cardinality:      l.Cardinality,
		}
		if l.FK != nil {
			raw, err := json.Marshal(l.FK)
			if err != nil {
				return nil, fmt.Errorf("seed: marshal FK config for %q: %w", l.APIName, err)
			}
			lt.ForeignKeyConfig = raw
		}
		if err := repo.CreateLinkType(ctx, lt); err != nil {
			return nil, fmt.Errorf("seed: create link type %q: %w", l.APIName, err)
		}
	}

	// 4b. Interfaces ----------------------------------------------------
	// HasOwner-style polymorphic interfaces let the interfaceBase
	// ObjectSet code path resolve to multiple implementing ObjectTypes at
	// query time. US-041 exercises this end-to-end from Playwright by
	// paging through loadObjectsOrInterfaces. Implementer apiNames were
	// validated against northwindSchemas() earlier in the function, so
	// otRIDByName lookups are guaranteed here.
	for _, iface := range northwindInterfaces() {
		ifaceRec := &oms.Interface{
			RID:         stableRID("interface", opts.OntologyAPIName+"-"+iface.APIName),
			OntologyRID: ontRID,
			APIName:     iface.APIName,
			DisplayName: iface.DisplayName,
		}
		if err := repo.CreateInterface(ctx, ifaceRec); err != nil {
			return nil, fmt.Errorf("seed: create interface %q: %w", iface.APIName, err)
		}
		for _, implName := range iface.Implementers {
			otRID, ok := otRIDByName[implName]
			if !ok {
				return nil, fmt.Errorf("seed: interface %q references unknown object type %q", iface.APIName, implName)
			}
			if err := repo.AttachInterface(ctx, &oms.ObjectTypeInterface{
				ObjectTypeRID: otRID,
				InterfaceRID:  ifaceRec.RID,
			}); err != nil {
				return nil, fmt.Errorf("seed: attach %q to %q: %w", implName, iface.APIName, err)
			}
		}
		logf("[seed] created interface %s with %d implementers", iface.APIName, len(iface.Implementers))
	}

	// 5. Action types ---------------------------------------------------
	// Each E2E-visible action lives under the northwind ontology so the
	// Action Console page (and the Playwright specs driving it) can
	// exercise apply + optimistic concurrency against the seeded
	// customers. Rules / parameters are stored as raw JSON because the
	// PGRepository does its own marshalling and the inputs are
	// hand-authored in schemas.go.
	for _, a := range northwindActionTypes() {
		at := &oms.ActionType{
			RID:         stableRID("action-type", opts.OntologyAPIName+"-"+a.APIName),
			OntologyRID: ontRID,
			APIName:     a.APIName,
			DisplayName: a.DisplayName,
			Description: a.Description,
			Status:      "ACTIVE",
			Parameters:  json.RawMessage(a.Parameters),
			Rules:       json.RawMessage(a.Rules),
		}
		if err := repo.CreateActionType(ctx, at); err != nil {
			return nil, fmt.Errorf("seed: create action type %q: %w", a.APIName, err)
		}
		logf("[seed] created action type %s", a.APIName)
	}

	// 5b. Security policies (US-081) -----------------------------------
	// Seed PROPERTY-scope (and eventually OBJECT-scope) policies so the
	// Playwright policy-column-hiding spec can verify per-role column
	// visibility end-to-end. The security_policies table is populated
	// directly (not via oms.Repository.CreateSecurityPolicy) so we
	// don't need to extend the seed's narrow repo interface.
	for _, sp := range northwindSecurityPolicies() {
		otRID, ok := otRIDByName[sp.ObjectTypeAPI]
		if !ok {
			return nil, fmt.Errorf("seed: security policy %q references unknown object type %q", sp.RID, sp.ObjectTypeAPI)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO security_policies (rid, object_type_rid, policy_type, rules)
			 VALUES ($1, $2, $3, $4)`,
			sp.RID, otRID, sp.PolicyType, sp.RulesJSON); err != nil {
			return nil, fmt.Errorf("seed: create security policy %q: %w", sp.RID, err)
		}
		logf("[seed] created security policy %s for %s", sp.RID, sp.ObjectTypeAPI)
	}

	// 6. Object history seed data --------------------------------------
	// One CREATE row per seed object per object type. Index rebuild will
	// replay these into Bleve via LoadLatestObjectStates.
	for _, s := range schemas {
		otRID := otRIDByName[s.APIName]
		for i, row := range s.SeedRows {
			body, err := json.Marshal(row)
			if err != nil {
				return nil, fmt.Errorf("seed: marshal %q row %d: %w", s.APIName, i, err)
			}
			hist := &oms.ObjectHistory{
				ObjectTypeRID: otRID,
				PrimaryKey:    fmt.Sprint(row[s.PrimaryKey]),
				Version:       1,
				NewState:      body,
				EditType:      "CREATE",
				Source:        oms.EditSourceUser,
				UserID:        "seed@test",
				RecordedAt:    time.Now().UTC(),
			}
			if err := repo.InsertObjectHistory(ctx, hist); err != nil {
				return nil, fmt.Errorf("seed: insert history %q[%s]: %w", s.APIName, hist.PrimaryKey, err)
			}
		}
		logf("[seed] wrote %d history rows for %s", len(s.SeedRows), s.APIName)
	}

	// 7. Test users -----------------------------------------------------
	userRepo := auth.NewPGUserRepository(pool)
	userIDs := make([]string, 0, len(opts.TestUsers))
	for _, u := range opts.TestUsers {
		id := u.Email
		hash, err := auth.HashPassword(u.Password)
		if err != nil {
			return nil, fmt.Errorf("seed: hash password for %q: %w", u.Email, err)
		}
		if err := userRepo.CreateUser(ctx, &auth.UserRecord{
			ID:           id,
			Email:        u.Email,
			Name:         u.Email,
			PasswordHash: hash,
		}); err != nil {
			return nil, fmt.Errorf("seed: create user %q: %w", u.Email, err)
		}
		for _, r := range u.Roles {
			if err := userRepo.UpsertUserRole(ctx, id, r); err != nil {
				return nil, fmt.Errorf("seed: grant %q to %q: %w", r, u.Email, err)
			}
		}
		userIDs = append(userIDs, id)
		logf("[seed] created user %s (%v)", u.Email, u.Roles)
	}

	// 7b. User marking grants (US-082) ----------------------------------
	// Insert custom markings into the markings table (ON CONFLICT to stay
	// idempotent against the base-seeded 5 canonical markings), then grant
	// each TestUser.Markings entry via user_markings.
	markingSeen := map[string]bool{}
	for _, u := range opts.TestUsers {
		for _, m := range u.Markings {
			if markingSeen[m] {
				continue
			}
			markingSeen[m] = true
			if _, err := pool.Exec(ctx,
				`INSERT INTO markings (name, display_name)
				 VALUES ($1, $1)
				 ON CONFLICT (name) DO NOTHING`, m); err != nil {
				return nil, fmt.Errorf("seed: upsert marking %q: %w", m, err)
			}
		}
	}
	for _, u := range opts.TestUsers {
		for _, m := range u.Markings {
			if _, err := pool.Exec(ctx,
				`INSERT INTO user_markings (user_id, marking_name, granted_by)
				 VALUES ($1, $2, 'seed')
				 ON CONFLICT DO NOTHING`, u.Email, m); err != nil {
				return nil, fmt.Errorf("seed: grant marking %q to %q: %w", m, u.Email, err)
			}
		}
		if len(u.Markings) > 0 {
			logf("[seed] granted markings %v to %s", u.Markings, u.Email)
		}
	}

	res := &Result{
		OntologyAPIName: opts.OntologyAPIName,
		OntologyRID:     ontRID,
		UserIDs:         userIDs,
	}
	for _, s := range schemas {
		res.ObjectTypes = append(res.ObjectTypes, s.APIName)
	}
	return res, nil
}

// wipe removes every row Seed() would otherwise duplicate-insert on a
// subsequent call. The queries are written so that repeated invocations
// on an empty database are still valid: every DELETE targets a specific
// ontology apiName / user email and is a no-op when no rows match.
func wipe(ctx context.Context, pool *pgxpool.Pool, opts Options) error {
	// Resolve the existing ontology RID (if any) so downstream deletes
	// can reference it directly. A missing ontology is not an error —
	// the caller just wants a clean slate.
	var ontRID string
	err := pool.QueryRow(ctx,
		`SELECT rid FROM ontologies WHERE api_name = $1`, opts.OntologyAPIName).
		Scan(&ontRID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No prior ontology — nothing to wipe on the OMS side.
	case err != nil:
		return fmt.Errorf("lookup ontology: %w", err)
	}
	if ontRID != "" {
		// Object history rows first (they reference object_type_rid but
		// there is no FK cascade set up, so we have to nuke them
		// manually).
		if _, err := pool.Exec(ctx,
			`DELETE FROM object_history
			 WHERE object_type_rid IN (
			   SELECT rid FROM object_types WHERE ontology_rid = $1
			 )`, ontRID); err != nil {
			return fmt.Errorf("delete object_history: %w", err)
		}
		// Link types (ontology scoped). link_edges cascades through
		// link_type_rid ON DELETE CASCADE so we do not need a separate
		// wipe for the edge table (see migration 000006_link_edges).
		if _, err := pool.Exec(ctx,
			`DELETE FROM link_types WHERE ontology_rid = $1`, ontRID); err != nil {
			return fmt.Errorf("delete link_types: %w", err)
		}
		// Action types (ontology scoped). action_logs references
		// action_types; delete the logs first to satisfy the FK before
		// dropping the types themselves.
		if _, err := pool.Exec(ctx,
			`DELETE FROM action_logs
			 WHERE action_type_rid IN (
			   SELECT rid FROM action_types WHERE ontology_rid = $1
			 )`, ontRID); err != nil {
			return fmt.Errorf("delete action_logs: %w", err)
		}
		if _, err := pool.Exec(ctx,
			`DELETE FROM action_types WHERE ontology_rid = $1`, ontRID); err != nil {
			return fmt.Errorf("delete action_types: %w", err)
		}
		// Interfaces (ontology scoped). object_type_interfaces rows
		// cascade automatically via ON DELETE CASCADE on interface_rid,
		// so a blanket delete of interfaces for the ontology leaves no
		// dangling join rows. Must happen before object_types because
		// there is no interfaces -> object_types FK chain to cascade
		// through.
		if _, err := pool.Exec(ctx,
			`DELETE FROM interfaces WHERE ontology_rid = $1`, ontRID); err != nil {
			return fmt.Errorf("delete interfaces: %w", err)
		}
		// Security policies (FK to object_types.rid) — delete before
		// object_types to satisfy the foreign key constraint.
		if _, err := pool.Exec(ctx,
			`DELETE FROM security_policies
			 WHERE object_type_rid IN (
			   SELECT rid FROM object_types WHERE ontology_rid = $1
			 )`, ontRID); err != nil {
			return fmt.Errorf("delete security_policies: %w", err)
		}
		// Object types — properties cascade via ON DELETE CASCADE (see
		// migration 000001_initial_schema.up.sql L33).
		if _, err := pool.Exec(ctx,
			`DELETE FROM object_types WHERE ontology_rid = $1`, ontRID); err != nil {
			return fmt.Errorf("delete object_types: %w", err)
		}
		// Finally the ontology row itself.
		if _, err := pool.Exec(ctx,
			`DELETE FROM ontologies WHERE rid = $1`, ontRID); err != nil {
			return fmt.Errorf("delete ontology: %w", err)
		}
	}

	// Test users are deleted by email so reruns with the same
	// DefaultOptions recreate them cleanly. user_roles and
	// user_ontology_roles cascade via ON DELETE CASCADE (migration
	// 000007_users_and_roles.up.sql).
	for _, u := range opts.TestUsers {
		if _, err := pool.Exec(ctx,
			`DELETE FROM users WHERE email = $1`, u.Email); err != nil {
			return fmt.Errorf("delete user %q: %w", u.Email, err)
		}
	}
	return nil
}

// stableRID produces a deterministic RID so reruns with the same opts
// converge to byte-identical PG rows. The final segment is the caller's
// slug rather than a uuid.New() — this matches the pattern used by the
// existing Northwind integration harness
// (test/northwind/northwind_test.go:1066).
func stableRID(resourceType, slug string) string {
	return fmt.Sprintf("ri.ontology.main.%s.%s", resourceType, slug)
}
