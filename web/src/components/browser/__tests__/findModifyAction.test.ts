import { describe, it, expect } from 'vitest';
import { findModifyActionForProperty } from '../findModifyAction';
import type { ActionType } from '../../../api/types';

const mkAction = (overrides: Partial<ActionType>): ActionType => ({
  rid: 'ri.at.test',
  apiName: 'modifyEmployee',
  displayName: 'Modify',
  status: 'ACTIVE',
  parameters: {},
  ...overrides,
});

describe('findModifyActionForProperty', () => {
  it('matches a modifyObject action that binds the property', () => {
    const action = mkAction({
      parameters: {
        primaryKey: { dataType: { type: 'string' }, required: true },
        name: { dataType: { type: 'string' }, required: false },
      },
      rules: [
        {
          type: 'modifyObject',
          objectType: 'Employee',
          propertyBindings: {
            name: { type: 'parameter', value: 'name' },
          },
        },
      ],
    });
    const match = findModifyActionForProperty([action], 'Employee', 'name');
    expect(match).not.toBeNull();
    expect(match!.propertyParams).toEqual({ name: 'name' });
    expect(match!.primaryKeyParam).toBe('primaryKey');
  });

  it('returns null when no matching action exists', () => {
    expect(findModifyActionForProperty([], 'Employee', 'name')).toBeNull();
  });

  it('skips actions whose rules target a different objectType', () => {
    const action = mkAction({
      rules: [
        {
          type: 'modifyObject',
          objectType: 'Customer',
          propertyBindings: { name: { type: 'parameter', value: 'name' } },
        },
      ],
      parameters: { primaryKey: { dataType: { type: 'string' }, required: true } },
    });
    expect(
      findModifyActionForProperty([action], 'Employee', 'name'),
    ).toBeNull();
  });

  it('returns null when the property is not bound by any rule', () => {
    const action = mkAction({
      rules: [
        {
          type: 'modifyObject',
          objectType: 'Employee',
          propertyBindings: { email: { type: 'parameter', value: 'email' } },
        },
      ],
      parameters: { primaryKey: { dataType: { type: 'string' }, required: true } },
    });
    expect(
      findModifyActionForProperty([action], 'Employee', 'name'),
    ).toBeNull();
  });

  it('falls back to <objectType>Id as primary key parameter', () => {
    const action = mkAction({
      parameters: {
        EmployeeId: { dataType: { type: 'string' }, required: true },
        name: { dataType: { type: 'string' }, required: false },
      },
      rules: [
        {
          type: 'modifyObject',
          objectType: 'Employee',
          propertyBindings: { name: { type: 'parameter', value: 'name' } },
        },
      ],
    });
    const match = findModifyActionForProperty([action], 'Employee', 'name');
    expect(match).not.toBeNull();
    expect(match!.primaryKeyParam).toBe('EmployeeId');
  });

  it('exposes every bound property when matching one', () => {
    const action = mkAction({
      parameters: {
        primaryKey: { dataType: { type: 'string' }, required: true },
        name: { dataType: { type: 'string' }, required: false },
        email: { dataType: { type: 'string' }, required: false },
      },
      rules: [
        {
          type: 'modifyObject',
          objectType: 'Employee',
          propertyBindings: {
            name: { type: 'parameter', value: 'name' },
            email: { type: 'parameter', value: 'email' },
          },
        },
      ],
    });
    const match = findModifyActionForProperty([action], 'Employee', 'name');
    expect(match!.propertyParams).toEqual({ name: 'name', email: 'email' });
  });
});
