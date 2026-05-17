import { describe, it, expect } from 'vitest';
import { buildContainsAnyTermClause } from '../whereBuilder';

// DOG-004: the Hermes dogfood captured the exact failing request shape
//
//   { type: 'containsAnyTerm', field: 'title', value: ['OpenAI'] }
//
// against the backend converter (pkg/oss/where/converter.go) which expects
// containsAnyTerm.value to be a string and unmarshals it via
// json.Unmarshal(..., &strVal). The array form returns
// `containsAnyTerm value must be a string: json: cannot unmarshal array
// into Go value of type string`, and the browser surfaces this as
// `INVALID_ARGUMENT: SearchObjectsFailed`. These BDD scenarios pin the
// frontend → backend contract: single-string serialisation regardless of
// how many whitespace-separated terms the operator typed.
describe('BDD: containsAnyTerm contract', () => {
  it('Given the user types one term, When buildContainsAnyTermClause runs, Then value is a string (not array)', () => {
    const clause = buildContainsAnyTermClause('title', 'OpenAI');
    expect(clause).toEqual({
      type: 'containsAnyTerm',
      field: 'title',
      value: 'OpenAI',
    });
  });

  it('Given the user types multiple terms, When buildContainsAnyTermClause runs, Then value is the normalized space-joined string the backend MatchQuery expects', () => {
    const clause = buildContainsAnyTermClause('title', '  OpenAI   Codex  ');
    expect(clause).toEqual({
      type: 'containsAnyTerm',
      field: 'title',
      value: 'OpenAI Codex',
    });
  });

  it('Given the user types only whitespace, When buildContainsAnyTermClause runs, Then the clause is undefined so the request omits a no-op where', () => {
    expect(buildContainsAnyTermClause('title', '')).toBeUndefined();
    expect(buildContainsAnyTermClause('title', '   ')).toBeUndefined();
  });
});
