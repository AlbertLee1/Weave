import { describe, it, expect } from 'vitest';
import { toApiName, toPluralName } from './naming';

describe('toApiName', () => {
  it('lowercases a single word', () => {
    expect(toApiName('Employee')).toBe('employee');
  });

  it('converts two words to camelCase', () => {
    expect(toApiName('Employee Department')).toBe('employeeDepartment');
  });

  it('converts multiple words to camelCase', () => {
    expect(toApiName('My Cool Type')).toBe('myCoolType');
  });

  it('returns empty string for empty input', () => {
    expect(toApiName('')).toBe('');
  });
});

describe('toPluralName', () => {
  it('appends s for regular words', () => {
    expect(toPluralName('Employee')).toBe('Employees');
  });

  it('changes y to ies for words ending in consonant + y', () => {
    expect(toPluralName('Company')).toBe('Companies');
  });

  it('appends es for words ending in s', () => {
    expect(toPluralName('Status')).toBe('Statuses');
  });

  it('appends es for words ending in ss', () => {
    expect(toPluralName('Address')).toBe('Addresses');
  });

  it('appends es for words ending in x', () => {
    expect(toPluralName('Box')).toBe('Boxes');
  });
});
