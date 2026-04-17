import { describe, it, expect } from 'vitest';
import {
  parseOpenApiYaml,
  extractEndpoints,
  groupByTag,
  buildRequestUrl,
  exampleForSchema,
  resolveRef,
} from '../openapiParser';

const SAMPLE_YAML = `
openapi: 3.0.3
info:
  title: Sample
  version: 1.0.0
tags:
  - name: Metadata
    description: read-only
  - name: Objects
    description: object ops
paths:
  /api/v2/ontologies:
    get:
      tags: [Metadata]
      operationId: listOntologies
      summary: List ontologies
      responses:
        '200':
          description: ok
  /api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}:
    get:
      tags: [Objects]
      operationId: getObject
      parameters:
        - name: ontologyApiName
          in: path
          required: true
          schema: { type: string }
        - name: objectType
          in: path
          required: true
          schema: { type: string }
        - name: primaryKey
          in: path
          required: true
          schema: { type: string }
        - name: select
          in: query
          schema: { type: string }
      responses:
        '200':
          description: ok
  /api/v2/ontologies/{ontologyApiName}/objects/{objectType}/search:
    post:
      tags: [Objects]
      operationId: searchObjects
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/SearchRequest'
      responses:
        '200':
          description: ok
components:
  schemas:
    SearchRequest:
      type: object
      properties:
        where:
          type: object
        pageSize:
          type: integer
`;

describe('parseOpenApiYaml', () => {
  it('parses a YAML spec into a plain object with paths and components', () => {
    const spec = parseOpenApiYaml(SAMPLE_YAML);
    expect(spec.openapi).toBe('3.0.3');
    expect(Object.keys(spec.paths ?? {})).toHaveLength(3);
    expect(spec.components?.schemas?.SearchRequest).toBeDefined();
  });
});

describe('extractEndpoints', () => {
  it('flattens paths × methods into an Endpoint[] list', () => {
    const spec = parseOpenApiYaml(SAMPLE_YAML);
    const endpoints = extractEndpoints(spec);
    expect(endpoints).toHaveLength(3);
    const get = endpoints.find((e) => e.operationId === 'listOntologies');
    expect(get?.method).toBe('GET');
    expect(get?.path).toBe('/api/v2/ontologies');
  });

  it('captures parameters and requestBody presence', () => {
    const spec = parseOpenApiYaml(SAMPLE_YAML);
    const endpoints = extractEndpoints(spec);
    const getObj = endpoints.find((e) => e.operationId === 'getObject');
    expect(getObj?.parameters.length).toBe(4);
    expect(getObj?.hasBody).toBe(false);

    const search = endpoints.find((e) => e.operationId === 'searchObjects');
    expect(search?.hasBody).toBe(true);
  });
});

describe('groupByTag', () => {
  it('groups endpoints by their first tag', () => {
    const spec = parseOpenApiYaml(SAMPLE_YAML);
    const groups = groupByTag(extractEndpoints(spec));
    expect(Object.keys(groups).sort()).toEqual(['Metadata', 'Objects']);
    expect(groups.Objects).toHaveLength(2);
  });

  it('places untagged endpoints under "Other"', () => {
    const spec = parseOpenApiYaml(
      `openapi: 3.0.3
info: { title: x, version: "1" }
paths:
  /foo:
    get: { responses: { '200': { description: ok } } }`,
    );
    const groups = groupByTag(extractEndpoints(spec));
    expect(groups.Other).toHaveLength(1);
  });
});

describe('buildRequestUrl', () => {
  it('substitutes path parameters and appends query string', () => {
    const url = buildRequestUrl(
      '/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}',
      { ontologyApiName: 'default', objectType: 'Employee', primaryKey: '42' },
      { select: 'id,name' },
    );
    expect(url).toBe('/api/v2/ontologies/default/objects/Employee/42?select=id%2Cname');
  });

  it('omits empty query parameters', () => {
    const url = buildRequestUrl('/foo/{id}', { id: '7' }, { a: '', b: 'x' });
    expect(url).toBe('/foo/7?b=x');
  });

  it('encodes path parameter values', () => {
    const url = buildRequestUrl('/f/{n}', { n: 'a b/c' }, {});
    expect(url).toBe('/f/a%20b%2Fc');
  });
});

describe('resolveRef / exampleForSchema', () => {
  it('resolveRef follows $ref into components.schemas', () => {
    const spec = parseOpenApiYaml(SAMPLE_YAML);
    const resolved = resolveRef(spec, '#/components/schemas/SearchRequest');
    expect(resolved).toBeDefined();
    expect(resolved?.type).toBe('object');
  });

  it('exampleForSchema produces a representative value for primitives', () => {
    expect(exampleForSchema({ type: 'string' }, {})).toBe('');
    expect(exampleForSchema({ type: 'integer' }, {})).toBe(0);
    expect(exampleForSchema({ type: 'boolean' }, {})).toBe(false);
  });

  it('exampleForSchema expands objects and $refs', () => {
    const spec = parseOpenApiYaml(SAMPLE_YAML);
    const example = exampleForSchema(
      { $ref: '#/components/schemas/SearchRequest' },
      spec,
    ) as Record<string, unknown>;
    expect(example).toHaveProperty('where');
    expect(example).toHaveProperty('pageSize');
    expect(example.pageSize).toBe(0);
  });
});
