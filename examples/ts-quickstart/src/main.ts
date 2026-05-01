// Weave TypeScript quickstart — a 5 minute hello-world.
//
// Talks to a local Weave server over its REST API using the global `fetch`
// (Node 18+ / modern browsers / Deno / Bun). No SDK package required —
// once you've gotten a feel for the API, generate a fully-typed SDK with
// `weave-cli sdk gen --lang ts --ontology <api-name>` for richer ergonomics.

interface Ontology {
  apiName: string;
  displayName: string;
}

interface ObjectType {
  apiName: string;
  displayName: string;
}

interface ObjectPage {
  data: Array<Record<string, unknown>>;
  nextPageToken?: string;
}

interface ListResponse<T> {
  data: T[];
  nextPageToken?: string;
}

const BASE_URL = process.env.WEAVE_BASE_URL ?? 'http://localhost:9117';
const TOKEN = process.env.WEAVE_TOKEN;

async function api<T>(path: string): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (TOKEN) headers['Authorization'] = `Bearer ${TOKEN}`;
  const res = await fetch(`${BASE_URL}${path}`, { headers });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`Weave ${res.status}: ${body || res.statusText}`);
  }
  return (await res.json()) as T;
}

async function listOntologies(): Promise<Ontology[]> {
  const resp = await api<ListResponse<Ontology>>('/api/v2/ontologies');
  return resp.data;
}

async function listObjectTypes(ontology: string): Promise<ObjectType[]> {
  const resp = await api<ListResponse<ObjectType>>(
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/objectTypes`,
  );
  return resp.data;
}

async function listObjects(
  ontology: string,
  objectType: string,
  pageSize: number,
): Promise<ObjectPage> {
  return api<ObjectPage>(
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/objects/${encodeURIComponent(objectType)}?pageSize=${pageSize}`,
  );
}

async function main(): Promise<void> {
  console.log('=== Ontologies ===');
  const ontologies = await listOntologies();
  for (const o of ontologies) {
    console.log(`- ${o.apiName}\t${o.displayName}`);
  }
  if (ontologies.length === 0) {
    console.log('(no ontologies — load a fixture e.g. testdata/northwind to see more)');
    return;
  }

  const ontology = ontologies[0]!.apiName;
  console.log(`=== Object types in ${ontology} ===`);
  const types = await listObjectTypes(ontology);
  for (const t of types) {
    console.log(`- ${t.apiName}\t${t.displayName}`);
  }
  if (types.length === 0) return;

  const objectType = types[0]!.apiName;
  console.log(`=== First 5 ${objectType} ===`);
  const page = await listObjects(ontology, objectType, 5);
  for (const row of page.data) {
    console.log(`- ${row['__primaryKey']}\t${JSON.stringify(row)}`);
  }
}

main().catch((err) => {
  console.error(err instanceof Error ? err.message : err);
  process.exit(1);
});
