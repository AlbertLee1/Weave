import { describe, expect, it } from 'vitest';

import {
  buildCreateCaseStudyRequest,
  buildCreateScenarioRequest,
  closeCreateCaseStudyDialog,
  closeCreateScenarioDialog,
  createCaseStudyDialogState,
  createScenarioDialogState,
  IMMUTABLE_SCENARIO_MESSAGE,
  assertScenarioMutable,
  isScenarioImmutable,
  openCreateCaseStudyDialog,
  openCreateScenarioDialog,
  ScenarioImmutableError,
  setCreateCaseStudyError,
  setCreateCaseStudyName,
  setCreateCaseStudySubmitting,
  setCreateScenarioError,
  setCreateScenarioName,
  setCreateScenarioSubmitting,
  validateDialogName,
} from './createDialogs';

const csRid = 'ri.vertex.main.case-study.cs-1';

describe('VTX-037 Create Case Study dialog state', () => {
  it('given_no_init_when_create_then_returns_closed_blank_dialog', () => {
    const s = createCaseStudyDialogState();
    expect(s).toEqual({ open: false, name: '', submitting: false, error: null });
  });

  it('given_no_args_when_open_then_opens_with_blank_name_and_cleared_state', () => {
    const opened = openCreateCaseStudyDialog();
    expect(opened).toEqual({ open: true, name: '', submitting: false, error: null });
  });

  it('given_no_args_when_close_then_returns_closed_blank_dialog', () => {
    const closed = closeCreateCaseStudyDialog();
    expect(closed).toEqual({ open: false, name: '', submitting: false, error: null });
  });

  it('given_open_dialog_when_setName_then_updates_name', () => {
    const s = openCreateCaseStudyDialog();
    const next = setCreateCaseStudyName(s, 'Hub Capacity');
    expect(next.name).toBe('Hub Capacity');
    expect(next.open).toBe(true);
  });

  it('given_open_dialog_when_setSubmitting_true_then_clears_error_and_marks_submitting', () => {
    let s = openCreateCaseStudyDialog();
    s = setCreateCaseStudyError(s, 'previous error');
    const next = setCreateCaseStudySubmitting(s, true);
    expect(next.submitting).toBe(true);
    expect(next.error).toBeNull();
  });

  it('given_submitting_dialog_when_setSubmitting_false_then_marks_not_submitting_preserving_error', () => {
    let s = openCreateCaseStudyDialog();
    s = setCreateCaseStudySubmitting(s, true);
    s = setCreateCaseStudyError(s, 'server 500');
    const next = setCreateCaseStudySubmitting(s, false);
    expect(next.submitting).toBe(false);
    expect(next.error).toBe('server 500');
  });

  it('given_open_dialog_when_setError_then_records_error_and_clears_submitting', () => {
    const s = setCreateCaseStudySubmitting(openCreateCaseStudyDialog(), true);
    const next = setCreateCaseStudyError(s, 'invalid name');
    expect(next.error).toBe('invalid name');
    expect(next.submitting).toBe(false);
  });

  it('given_dialog_with_error_when_setError_null_then_clears_error', () => {
    let s = openCreateCaseStudyDialog();
    s = setCreateCaseStudyError(s, 'oops');
    const next = setCreateCaseStudyError(s, null);
    expect(next.error).toBeNull();
  });
});

describe('VTX-037 Create Scenario dialog state', () => {
  it('given_no_init_when_create_then_returns_closed_blank_dialog_without_caseStudy', () => {
    const s = createScenarioDialogState();
    expect(s).toEqual({
      open: false,
      caseStudyRid: null,
      name: '',
      submitting: false,
      error: null,
    });
  });

  it('given_caseStudyRid_when_open_then_opens_scoped_to_rid', () => {
    const s = openCreateScenarioDialog(csRid);
    expect(s).toEqual({
      open: true,
      caseStudyRid: csRid,
      name: '',
      submitting: false,
      error: null,
    });
  });

  it('given_empty_rid_when_open_then_throws', () => {
    expect(() => openCreateScenarioDialog('')).toThrow(/case study/i);
  });

  it('given_reopen_with_same_rid_when_open_then_returns_blank_dialog', () => {
    const first = openCreateScenarioDialog(csRid);
    const dirty = setCreateScenarioError(
      setCreateScenarioName(first, 'leftover'),
      'stale',
    );
    expect(dirty.name).toBe('leftover');
    const reopened = openCreateScenarioDialog(csRid);
    expect(reopened.name).toBe('');
    expect(reopened.error).toBeNull();
  });

  it('given_no_args_when_close_then_resets_to_closed_blank_state', () => {
    const closed = closeCreateScenarioDialog();
    expect(closed).toEqual({
      open: false,
      caseStudyRid: null,
      name: '',
      submitting: false,
      error: null,
    });
  });

  it('given_open_dialog_when_setName_then_updates_name_preserving_caseStudy', () => {
    const s = openCreateScenarioDialog(csRid);
    const next = setCreateScenarioName(s, 'Scenario A');
    expect(next.name).toBe('Scenario A');
    expect(next.caseStudyRid).toBe(csRid);
  });

  it('given_open_dialog_when_setSubmitting_true_then_clears_error_and_marks_submitting', () => {
    let s = openCreateScenarioDialog(csRid);
    s = setCreateScenarioError(s, 'previous');
    const next = setCreateScenarioSubmitting(s, true);
    expect(next.submitting).toBe(true);
    expect(next.error).toBeNull();
  });

  it('given_open_dialog_when_setError_then_records_error_and_clears_submitting', () => {
    const s = setCreateScenarioSubmitting(openCreateScenarioDialog(csRid), true);
    const next = setCreateScenarioError(s, 'duplicate name');
    expect(next.error).toBe('duplicate name');
    expect(next.submitting).toBe(false);
  });
});

describe('VTX-037 validateDialogName', () => {
  it('given_blank_name_when_validate_then_invalid_with_required_reason', () => {
    expect(validateDialogName('')).toEqual({ valid: false, reason: 'required' });
  });

  it('given_whitespace_only_when_validate_then_invalid_with_required_reason', () => {
    expect(validateDialogName('   ')).toEqual({ valid: false, reason: 'required' });
  });

  it('given_normal_name_when_validate_then_valid', () => {
    expect(validateDialogName('Hub Capacity')).toEqual({ valid: true });
  });

  it('given_name_trimmed_within_limit_when_validate_then_valid', () => {
    expect(validateDialogName('  Scenario A  ')).toEqual({ valid: true });
  });

  it('given_name_exceeding_max_length_when_validate_then_invalid_with_too_long_reason', () => {
    const long = 'a'.repeat(129);
    expect(validateDialogName(long)).toEqual({ valid: false, reason: 'too_long' });
  });

  it('given_name_at_max_length_after_trim_when_validate_then_valid', () => {
    const exact = 'a'.repeat(128);
    expect(validateDialogName(exact)).toEqual({ valid: true });
  });
});

describe('VTX-037 buildCreateCaseStudyRequest', () => {
  it('given_name_and_ontology_when_build_then_posts_to_case_studies_endpoint', () => {
    const req = buildCreateCaseStudyRequest({
      name: 'Hub Capacity',
      ontologyRid: 'ri.ontology.main.ontology.air',
    });
    expect(req).toEqual({
      method: 'POST',
      path: '/api/vertex/v1/case-studies',
      body: {
        name: 'Hub Capacity',
        ontologyRid: 'ri.ontology.main.ontology.air',
      },
    });
  });

  it('given_padded_name_when_build_then_trims_name_before_sending', () => {
    const req = buildCreateCaseStudyRequest({
      name: '  Hub Capacity  ',
      ontologyRid: 'ri.ontology.main.ontology.air',
    });
    expect(req.body).toEqual({
      name: 'Hub Capacity',
      ontologyRid: 'ri.ontology.main.ontology.air',
    });
  });

  it('given_blank_name_when_build_then_throws', () => {
    expect(() =>
      buildCreateCaseStudyRequest({ name: '   ', ontologyRid: 'ri.ontology.main.ontology.air' }),
    ).toThrow(/name/i);
  });

  it('given_blank_ontologyRid_when_build_then_throws', () => {
    expect(() =>
      buildCreateCaseStudyRequest({ name: 'Hub', ontologyRid: '' }),
    ).toThrow(/ontologyRid/i);
  });
});

describe('VTX-037 buildCreateScenarioRequest', () => {
  it('given_caseStudy_and_name_when_build_then_posts_to_nested_scenarios_endpoint', () => {
    const req = buildCreateScenarioRequest({
      caseStudyRid: csRid,
      name: 'Scenario A',
      parentOntologyCommit: 'commit-123',
    });
    expect(req).toEqual({
      method: 'POST',
      path: `/api/vertex/v1/case-studies/${encodeURIComponent(csRid)}/scenarios`,
      body: { name: 'Scenario A', parentOntologyCommit: 'commit-123' },
    });
  });

  it('given_caseStudyRid_with_special_chars_when_build_then_encodes_path_segment', () => {
    const odd = 'ri.vertex.main.case-study.with space';
    const req = buildCreateScenarioRequest({
      caseStudyRid: odd,
      name: 'Scenario A',
      parentOntologyCommit: 'commit-123',
    });
    expect(req.path).toBe(
      `/api/vertex/v1/case-studies/${encodeURIComponent(odd)}/scenarios`,
    );
    expect(req.path).toContain('with%20space');
  });

  it('given_padded_name_when_build_then_trims_name_before_sending', () => {
    const req = buildCreateScenarioRequest({
      caseStudyRid: csRid,
      name: '  Scenario A  ',
      parentOntologyCommit: 'commit-123',
    });
    expect(req.body.name).toBe('Scenario A');
  });

  it('given_blank_caseStudyRid_when_build_then_throws', () => {
    expect(() =>
      buildCreateScenarioRequest({
        caseStudyRid: '',
        name: 'Scenario A',
        parentOntologyCommit: 'commit-123',
      }),
    ).toThrow(/caseStudyRid/i);
  });

  it('given_blank_name_when_build_then_throws', () => {
    expect(() =>
      buildCreateScenarioRequest({
        caseStudyRid: csRid,
        name: '   ',
        parentOntologyCommit: 'commit-123',
      }),
    ).toThrow(/name/i);
  });

  it('given_blank_parentOntologyCommit_when_build_then_throws', () => {
    expect(() =>
      buildCreateScenarioRequest({
        caseStudyRid: csRid,
        name: 'Scenario A',
        parentOntologyCommit: '',
      }),
    ).toThrow(/parentOntologyCommit/i);
  });
});

describe('VTX-037 immutable scenario guard', () => {
  it('given_mutable_scenario_when_isScenarioImmutable_then_false', () => {
    expect(
      isScenarioImmutable({ rid: 'ri.s.1', name: 's', immutable: false }),
    ).toBe(false);
  });

  it('given_scenario_without_immutable_flag_when_isScenarioImmutable_then_false', () => {
    expect(isScenarioImmutable({ rid: 'ri.s.1', name: 's' })).toBe(false);
  });

  it('given_immutable_scenario_when_isScenarioImmutable_then_true', () => {
    expect(
      isScenarioImmutable({ rid: 'ri.s.1', name: 's', immutable: true }),
    ).toBe(true);
  });

  it('given_mutable_scenario_when_assertScenarioMutable_then_does_not_throw', () => {
    expect(() =>
      assertScenarioMutable({ rid: 'ri.s.1', name: 's', immutable: false }),
    ).not.toThrow();
  });

  it('given_immutable_scenario_when_assertScenarioMutable_then_throws_ScenarioImmutableError_with_message', () => {
    const scenario = { rid: 'ri.s.frozen', name: 'Frozen', immutable: true };
    let caught: unknown;
    try {
      assertScenarioMutable(scenario);
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(ScenarioImmutableError);
    expect((caught as ScenarioImmutableError).message).toBe(
      IMMUTABLE_SCENARIO_MESSAGE,
    );
    expect((caught as ScenarioImmutableError).scenarioRid).toBe('ri.s.frozen');
  });

  it('IMMUTABLE_SCENARIO_MESSAGE matches the spec text', () => {
    expect(IMMUTABLE_SCENARIO_MESSAGE).toBe(
      'Scenario is immutable. Duplicate to modify.',
    );
  });
});
