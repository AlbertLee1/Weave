package main

import (
	"context"
	"time"

	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/cellsec"
	"github.com/liyang/weave/pkg/compliance"
	"github.com/liyang/weave/pkg/masking"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rls"
)

// buildComplianceGenerator composes a compliance.Generator from
// whichever subsystems are wired on deps. Every source is independently
// optional — a nil subsystem produces a nil source and the generator
// emits an empty section for it. Returns nil only when NO source could
// be wired so callers can skip mounting the route entirely in fully
// degraded mode.
func buildComplianceGenerator(
	auditStore audit.Store,
	markingRepo auth.MarkingRepository,
	markingAdmin auth.MarkingGrantAdminRepository,
	omsRepo oms.Repository,
	rowPolicies rls.Store,
	columnMasks masking.Store,
	cellMasks cellsec.Store,
) *compliance.Generator {
	g := compliance.New()
	var wired bool
	if auditStore != nil {
		g.Audit = &complianceAuditSource{store: auditStore}
		wired = true
	}
	if markingRepo != nil {
		g.Markings = &complianceMarkingSource{repo: markingRepo, admin: markingAdmin}
		wired = true
	}
	if omsRepo != nil {
		g.ObjectTypes = &complianceObjectTypeSource{repo: omsRepo}
		wired = true
	}
	if rowPolicies != nil || columnMasks != nil || cellMasks != nil {
		g.Policies = &compliancePolicySource{
			rowPolicies: rowPolicies,
			columnMasks: columnMasks,
			cellMasks:   cellMasks,
		}
		wired = true
	}
	if !wired {
		return nil
	}
	return g
}

// complianceAuditSource adapts audit.Store.List to the compliance
// report's time-windowed query. The PageSize is capped at 50k so
// month-scale windows on a busy deployment stay within memory; callers
// wanting finer-grained evidence can narrow the window.
type complianceAuditSource struct{ store audit.Store }

func (s *complianceAuditSource) ListEvents(ctx context.Context, from, to time.Time) ([]audit.AuditEvent, error) {
	f := audit.ListFilter{PageSize: 50_000}
	if !from.IsZero() {
		t := from
		f.From = &t
	}
	if !to.IsZero() {
		t := to
		f.To = &t
	}
	return s.store.List(ctx, f)
}

// complianceMarkingSource adapts the combined Marking + admin repos into
// the compliance source. When admin is nil CountGrants returns 0 for
// every marking — the Markings section still surfaces the definitions
// but the grant counts are unknown.
type complianceMarkingSource struct {
	repo  auth.MarkingRepository
	admin auth.MarkingGrantAdminRepository
}

func (s *complianceMarkingSource) ListMarkings(ctx context.Context) ([]compliance.MarkingInfo, error) {
	markings, err := s.repo.ListMarkings(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]compliance.MarkingInfo, 0, len(markings))
	for _, m := range markings {
		out = append(out, compliance.MarkingInfo{
			Name:        m.Name,
			DisplayName: m.DisplayName,
			Description: m.Description,
			Color:       m.Color,
		})
	}
	return out, nil
}

func (s *complianceMarkingSource) CountGrants(ctx context.Context, name string) (int, error) {
	if s.admin == nil {
		return 0, nil
	}
	grants, err := s.admin.ListGrantsByMarking(ctx, name)
	if err != nil {
		return 0, err
	}
	return len(grants), nil
}

// complianceObjectTypeSource walks every ontology and sums the
// ObjectType counts. Used as the denominator for the PolicyCoverage
// ratio. Executes at most two round trips per ontology (ListOntologies
// then ListObjectTypes per ontology) and the admin surface is low-QPS
// so a cache is not needed.
type complianceObjectTypeSource struct{ repo oms.Repository }

func (s *complianceObjectTypeSource) CountObjectTypes(ctx context.Context) (int, error) {
	onts, err := s.repo.ListOntologies(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, ont := range onts {
		ots, err := s.repo.ListObjectTypes(ctx, ont.RID)
		if err != nil {
			return 0, err
		}
		total += len(ots)
	}
	return total, nil
}

// compliancePolicySource reads the three security-surface stores and
// returns per-surface (count, covered-ObjectTypeRIDs) pairs. Each store
// is independently optional; a nil store contributes (0, nil).
type compliancePolicySource struct {
	rowPolicies rls.Store
	columnMasks masking.Store
	cellMasks   cellsec.Store
}

func (s *compliancePolicySource) RowPolicyStats(ctx context.Context) (int, []string, error) {
	if s.rowPolicies == nil {
		return 0, nil, nil
	}
	policies, err := s.rowPolicies.List(ctx)
	if err != nil {
		return 0, nil, err
	}
	out := make([]string, 0, len(policies))
	for _, p := range policies {
		out = append(out, p.ObjectTypeRID)
	}
	return len(policies), out, nil
}

func (s *compliancePolicySource) ColumnMaskStats(ctx context.Context) (int, []string, error) {
	if s.columnMasks == nil {
		return 0, nil, nil
	}
	masks, err := s.columnMasks.List(ctx)
	if err != nil {
		return 0, nil, err
	}
	out := make([]string, 0, len(masks))
	for _, m := range masks {
		out = append(out, m.ObjectTypeRID)
	}
	return len(masks), out, nil
}

func (s *compliancePolicySource) CellMaskStats(ctx context.Context) (int, []string, error) {
	if s.cellMasks == nil {
		return 0, nil, nil
	}
	masks, err := s.cellMasks.List(ctx)
	if err != nil {
		return 0, nil, err
	}
	out := make([]string, 0, len(masks))
	for _, m := range masks {
		out = append(out, m.ObjectTypeRID)
	}
	return len(masks), out, nil
}
