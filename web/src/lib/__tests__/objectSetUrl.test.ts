import { describe, it, expect } from 'vitest';
import type { ObjectSetDefinition } from '../../api/types';
import {
  OBJECT_SET_URL_PARAM,
  encodeDefinitionToParam,
  decodeDefinitionFromParam,
  parseDefinitionFromSearch,
  serializeDefinitionToSearch,
} from '../objectSetUrl';

describe('encodeDefinitionToParam / decodeDefinitionFromParam', () => {
  it('round-trips a base definition', () => {
    const def: ObjectSetDefinition = { type: 'base', objectType: 'Employee' };
    const param = encodeDefinitionToParam(def);
    expect(param).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(decodeDefinitionFromParam(param)).toEqual(def);
  });

  it('round-trips a nested filter+union definition', () => {
    const def: ObjectSetDefinition = {
      type: 'union',
      objectSets: [
        {
          type: 'filter',
          objectSet: { type: 'base', objectType: 'Employee' },
          where: { type: 'eq', field: 'role', value: 'engineer' },
        },
        { type: 'base', objectType: 'Department' },
      ],
    };
    const param = encodeDefinitionToParam(def);
    expect(decodeDefinitionFromParam(param)).toEqual(def);
  });

  it('returns null for malformed param', () => {
    expect(decodeDefinitionFromParam('not-base64-or-json!!!')).toBeNull();
  });

  it('returns null for empty param', () => {
    expect(decodeDefinitionFromParam('')).toBeNull();
  });

  it('returns null for valid base64 but invalid JSON', () => {
    // base64url("not json")
    const garbage = btoa('not json').replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    expect(decodeDefinitionFromParam(garbage)).toBeNull();
  });

  it('returns null when payload is JSON but not an ObjectSetDefinition shape', () => {
    const garbage = btoa(JSON.stringify({ foo: 'bar' }))
      .replace(/\+/g, '-')
      .replace(/\//g, '_')
      .replace(/=+$/, '');
    expect(decodeDefinitionFromParam(garbage)).toBeNull();
  });

  it('uses URL-safe base64 (no +, /, =)', () => {
    // Construct a definition whose JSON contains characters that produce + and /
    // in standard base64 encoding. Long arrays / Unicode usually produce them.
    const def: ObjectSetDefinition = {
      type: 'filter',
      objectSet: { type: 'base', objectType: 'Item' },
      where: { type: 'eq', field: 'name', value: '中文?+/=' },
    };
    const param = encodeDefinitionToParam(def);
    expect(param).not.toMatch(/[+/=]/);
    expect(decodeDefinitionFromParam(param)).toEqual(def);
  });
});

describe('serializeDefinitionToSearch / parseDefinitionFromSearch', () => {
  it('emits a query string with the configured param name', () => {
    const def: ObjectSetDefinition = { type: 'base', objectType: 'Employee' };
    const search = serializeDefinitionToSearch(def);
    expect(search).toMatch(new RegExp(`^\\?${OBJECT_SET_URL_PARAM}=`));
  });

  it('round-trips through parseDefinitionFromSearch', () => {
    const def: ObjectSetDefinition = {
      type: 'filter',
      objectSet: { type: 'base', objectType: 'Employee' },
      where: { type: 'eq', field: 'role', value: 'engineer' },
    };
    const search = serializeDefinitionToSearch(def);
    expect(parseDefinitionFromSearch(search)).toEqual(def);
  });

  it('returns null when the param is missing', () => {
    expect(parseDefinitionFromSearch('')).toBeNull();
    expect(parseDefinitionFromSearch('?other=1')).toBeNull();
  });

  it('preserves other query params alongside the def param', () => {
    const def: ObjectSetDefinition = { type: 'base', objectType: 'Employee' };
    const search = serializeDefinitionToSearch(def);
    const merged = `${search}&page=2`;
    expect(parseDefinitionFromSearch(merged)).toEqual(def);
  });

  it('parses a leading-? and bare query string identically', () => {
    const def: ObjectSetDefinition = { type: 'base', objectType: 'X' };
    const search = serializeDefinitionToSearch(def);
    const bare = search.replace(/^\?/, '');
    expect(parseDefinitionFromSearch(search)).toEqual(def);
    expect(parseDefinitionFromSearch(bare)).toEqual(def);
  });
});
