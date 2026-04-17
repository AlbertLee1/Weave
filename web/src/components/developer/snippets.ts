export interface SnippetRequest {
  method: string;
  url: string;
  body: string;
  token: string;
}

export interface Snippets {
  curl: string;
  typescript: string;
  python: string;
  go: string;
}

function methodHasBody(method: string): boolean {
  const m = method.toUpperCase();
  return m === 'POST' || m === 'PUT' || m === 'PATCH' || m === 'DELETE';
}

function shellEscape(s: string): string {
  return `'${s.replace(/'/g, `'\\''`)}'`;
}

export function buildSnippets(req: SnippetRequest): Snippets {
  const { method, url, body, token } = req;
  const upper = method.toUpperCase();
  const includeBody = methodHasBody(upper) && body.trim().length > 0;

  return {
    curl: buildCurl(upper, url, includeBody ? body : '', token),
    typescript: buildTypeScript(upper, url, includeBody ? body : '', token),
    python: buildPython(upper, url, includeBody ? body : '', token),
    go: buildGo(upper, url, includeBody ? body : '', token),
  };
}

function buildCurl(method: string, url: string, body: string, token: string): string {
  const lines: string[] = [`curl -X ${method} ${shellEscape(url)}`];
  if (token) lines.push(`  -H 'Authorization: Bearer ${token}'`);
  if (body) {
    lines.push(`  -H 'Content-Type: application/json'`);
    lines.push(`  -d ${shellEscape(body)}`);
  }
  return lines.join(' \\\n');
}

function buildTypeScript(method: string, url: string, body: string, token: string): string {
  const headers: string[] = [];
  if (body) headers.push(`    'Content-Type': 'application/json',`);
  if (token) headers.push(`    Authorization: \`Bearer ${token}\`,`);
  const bodyLine = body ? `  body: JSON.stringify(${body.trim()}),\n` : '';
  const headerBlock = headers.length
    ? `  headers: {\n${headers.join('\n')}\n  },\n`
    : '';
  return `const res = await fetch(${JSON.stringify(url)}, {
  method: '${method}',
${headerBlock}${bodyLine}});
const data = await res.json();
console.log(data);`;
}

function buildPython(method: string, url: string, body: string, token: string): string {
  const lines: string[] = ['import requests', ''];
  const headers: string[] = [];
  if (token) headers.push(`    'Authorization': 'Bearer ${token}'`);
  if (body) headers.push(`    'Content-Type': 'application/json'`);
  const headerArg = headers.length
    ? `, headers={\n${headers.join(',\n')}\n}`
    : '';
  const bodyArg = body ? `, json=${body.trim() || '{}'}` : '';
  lines.push(
    `res = requests.${method.toLowerCase()}(${JSON.stringify(url)}${headerArg}${bodyArg})`,
  );
  lines.push('print(res.status_code, res.json())');
  return lines.join('\n');
}

function buildGo(method: string, url: string, body: string, token: string): string {
  const hasBody = body.length > 0;
  return `package main

import (
\t"bytes"
\t"fmt"
\t"io"
\t"net/http"
)

func main() {
\t${hasBody ? `body := []byte(\`${body.replace(/`/g, '` + "`" + `')}\`)` : 'var body []byte'}
\treq, _ := http.NewRequest("${method}", ${JSON.stringify(url)}, ${hasBody ? 'bytes.NewReader(body)' : 'nil'})
${token ? `\treq.Header.Set("Authorization", "Bearer ${token}")\n` : ''}${hasBody ? '\treq.Header.Set("Content-Type", "application/json")\n' : ''}\tres, err := http.DefaultClient.Do(req)
\tif err != nil { panic(err) }
\tdefer res.Body.Close()
\tout, _ := io.ReadAll(res.Body)
\tfmt.Println(res.StatusCode, string(out))
}`;
}
