import { describe, it, expect } from 'vitest';
import { buildSnippets } from '../snippets';

describe('buildSnippets', () => {
  const baseReq = {
    method: 'POST',
    url: '/api/v2/ontologies/default/actions/applyAction',
    body: JSON.stringify({ parameters: { name: 'Alice' } }, null, 2),
    token: 'abc123',
  };

  it('generates a curl command with method, URL, headers, and body', () => {
    const s = buildSnippets(baseReq);
    expect(s.curl).toContain("curl -X POST");
    expect(s.curl).toContain("/api/v2/ontologies/default/actions/applyAction");
    expect(s.curl).toContain("Authorization: Bearer abc123");
    expect(s.curl).toContain("Content-Type: application/json");
    expect(s.curl).toContain("Alice");
  });

  it('omits Authorization header when token is empty', () => {
    const s = buildSnippets({ ...baseReq, token: '' });
    expect(s.curl).not.toContain('Authorization');
    expect(s.typescript).not.toContain('Bearer');
  });

  it('generates TypeScript fetch code', () => {
    const s = buildSnippets(baseReq);
    expect(s.typescript).toContain("await fetch(");
    expect(s.typescript).toContain("method: 'POST'");
    expect(s.typescript).toContain("JSON.stringify");
  });

  it('omits body for GET requests', () => {
    const s = buildSnippets({
      method: 'GET',
      url: '/api/v2/ontologies',
      body: '',
      token: 'abc',
    });
    expect(s.curl).not.toContain("-d ");
    expect(s.typescript).not.toContain('body:');
    expect(s.python).not.toContain('json=');
    expect(s.go).not.toContain('bytes.NewReader');
  });

  it('generates Python requests code', () => {
    const s = buildSnippets(baseReq);
    expect(s.python).toContain("import requests");
    expect(s.python).toContain("requests.post(");
    expect(s.python).toContain("json=");
  });

  it('generates Go net/http code', () => {
    const s = buildSnippets(baseReq);
    expect(s.go).toContain('package main');
    expect(s.go).toContain('http.NewRequest');
    expect(s.go).toContain('"POST"');
  });
});
