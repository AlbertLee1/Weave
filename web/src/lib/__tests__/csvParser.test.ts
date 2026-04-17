import { describe, it, expect } from 'vitest';
import {
  parseCsv,
  autoMapColumns,
  convertCellValue,
  validateCell,
} from '../csvParser';

describe('parseCsv', () => {
  it('parses a simple CSV with header and rows', () => {
    const text = 'id,name,age\r\n1,Alice,30\r\n2,Bob,25';
    const result = parseCsv(text);
    expect(result.headers).toEqual(['id', 'name', 'age']);
    expect(result.rows).toEqual([
      { id: '1', name: 'Alice', age: '30' },
      { id: '2', name: 'Bob', age: '25' },
    ]);
  });

  it('handles LF-only line endings', () => {
    const text = 'a,b\n1,2\n3,4';
    const result = parseCsv(text);
    expect(result.headers).toEqual(['a', 'b']);
    expect(result.rows).toHaveLength(2);
  });

  it('handles quoted fields with commas and newlines', () => {
    const text = 'id,note\r\n1,"hello, world"\r\n2,"line1\nline2"';
    const result = parseCsv(text);
    expect(result.rows[0].note).toBe('hello, world');
    expect(result.rows[1].note).toBe('line1\nline2');
  });

  it('handles escaped double-quotes inside quoted fields', () => {
    const text = 'id,q\r\n1,"he said ""hi"""';
    const result = parseCsv(text);
    expect(result.rows[0].q).toBe('he said "hi"');
  });

  it('ignores blank trailing lines', () => {
    const text = 'a,b\r\n1,2\r\n\r\n';
    const result = parseCsv(text);
    expect(result.rows).toHaveLength(1);
  });

  it('pads missing fields with empty string', () => {
    const text = 'a,b,c\r\n1,2';
    const result = parseCsv(text);
    expect(result.rows[0]).toEqual({ a: '1', b: '2', c: '' });
  });

  it('returns empty result for empty input', () => {
    const result = parseCsv('');
    expect(result.headers).toEqual([]);
    expect(result.rows).toEqual([]);
  });
});

describe('autoMapColumns', () => {
  it('maps CSV headers to properties by exact api-name match', () => {
    const headers = ['id', 'name', 'age'];
    const properties = ['id', 'name', 'email'];
    expect(autoMapColumns(headers, properties)).toEqual({
      id: 'id',
      name: 'name',
      age: '',
    });
  });

  it('matches case-insensitively', () => {
    const headers = ['Employee_Id', 'Name'];
    const properties = ['employee_id', 'name'];
    const mapping = autoMapColumns(headers, properties);
    expect(mapping['Employee_Id']).toBe('employee_id');
    expect(mapping['Name']).toBe('name');
  });

  it('matches after stripping underscores, spaces, and dashes', () => {
    const headers = ['first name', 'last-name', 'date_of_birth'];
    const properties = ['firstName', 'lastName', 'dateOfBirth'];
    const mapping = autoMapColumns(headers, properties);
    expect(mapping['first name']).toBe('firstName');
    expect(mapping['last-name']).toBe('lastName');
    expect(mapping['date_of_birth']).toBe('dateOfBirth');
  });

  it('leaves unmatched headers mapped to empty', () => {
    expect(autoMapColumns(['xyz'], ['id'])).toEqual({ xyz: '' });
  });
});

describe('convertCellValue', () => {
  it('returns {value: null} for empty-string on any field', () => {
    expect(convertCellValue('', 'string')).toEqual({ value: null });
    expect(convertCellValue('', 'integer')).toEqual({ value: null });
  });

  it('coerces integers and rejects NaN', () => {
    expect(convertCellValue('42', 'integer')).toEqual({ value: 42 });
    expect(convertCellValue('3.5', 'integer')).toHaveProperty('error');
    expect(convertCellValue('abc', 'integer')).toHaveProperty('error');
  });

  it('coerces longs like integers', () => {
    expect(convertCellValue('9999999999', 'long')).toEqual({ value: 9999999999 });
  });

  it('coerces doubles and floats', () => {
    expect(convertCellValue('3.14', 'double')).toEqual({ value: 3.14 });
    expect(convertCellValue('3.14', 'float')).toEqual({ value: 3.14 });
    expect(convertCellValue('nope', 'double')).toHaveProperty('error');
  });

  it('coerces booleans from common truthy/falsy strings', () => {
    expect(convertCellValue('true', 'boolean')).toEqual({ value: true });
    expect(convertCellValue('FALSE', 'boolean')).toEqual({ value: false });
    expect(convertCellValue('1', 'boolean')).toEqual({ value: true });
    expect(convertCellValue('0', 'boolean')).toEqual({ value: false });
    expect(convertCellValue('yes', 'boolean')).toEqual({ value: true });
    expect(convertCellValue('no', 'boolean')).toEqual({ value: false });
    expect(convertCellValue('maybe', 'boolean')).toHaveProperty('error');
  });

  it('passes strings through unchanged', () => {
    expect(convertCellValue('hello', 'string')).toEqual({ value: 'hello' });
  });

  it('validates ISO date strings', () => {
    expect(convertCellValue('2024-01-15', 'date')).toEqual({
      value: '2024-01-15',
    });
    expect(convertCellValue('not-a-date', 'date')).toHaveProperty('error');
  });

  it('validates ISO timestamp strings', () => {
    expect(
      convertCellValue('2024-01-15T10:30:00Z', 'timestamp'),
    ).toEqual({ value: '2024-01-15T10:30:00Z' });
    expect(convertCellValue('not-a-ts', 'timestamp')).toHaveProperty('error');
  });

  it('returns value unchanged for unsupported types', () => {
    expect(convertCellValue('foo', 'struct')).toEqual({ value: 'foo' });
  });
});

describe('validateCell', () => {
  it('returns null warning for a successful conversion', () => {
    expect(validateCell('42', 'integer')).toBeNull();
  });

  it('returns a warning message for a bad conversion', () => {
    const warning = validateCell('abc', 'integer');
    expect(warning).toBeTruthy();
    expect(typeof warning).toBe('string');
  });

  it('returns null for null/empty values regardless of type', () => {
    expect(validateCell('', 'integer')).toBeNull();
  });
});
