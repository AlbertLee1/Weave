import { describe, it, expect } from 'vitest';
import { splitCamelCase } from '../breadcrumb';

describe('splitCamelCase', () => {
  it.each([
    ['ObjectTypes', 'Object Types'],
    ['linkTypes', 'Link Types'],
    ['valueTypes', 'Value Types'],
    ['saga-dlq', 'Saga DLQ'],
    ['iotDemo', 'Iot Demo'],
    ['admin', 'Admin'],
    ['objectsets', 'Objectsets'],
  ])('splitCamelCase(%s) -> %s', (input, expected) => {
    expect(splitCamelCase(input)).toBe(expected);
  });
});
