// Weave TypeScript OSDK quickstart — a 5 minute hello-world.
//
// Demonstrates the OSDK's four typed clients (Object / Action / Function /
// Subscribe). The OSDK is deliberately self-contained — uses the global
// `fetch` (Node 18+ / modern browsers / Deno / Bun), no extra deps.

import { WeaveClient } from './client.js';
import type { ObjectRow } from './openapi.js';

interface CustomerRow extends ObjectRow {
  __primaryKey?: string;
  companyName?: string;
}

const baseUrl = process.env['WEAVE_BASE_URL'] ?? 'http://localhost:9117';
const token = process.env['WEAVE_TOKEN'];

async function main(): Promise<void> {
  const client = new WeaveClient({ baseUrl, ...(token ? { token } : {}) });

  console.log('=== Ontologies ===');
  const ontologies = await client.listOntologies();
  for (const o of ontologies) {
    console.log(`- ${o.apiName}\t${o.displayName}`);
  }
  if (ontologies.length === 0) {
    console.log('(no ontologies — load a fixture e.g. testdata/northwind to see more)');
    return;
  }

  const ontology = ontologies[0]!.apiName;
  console.log(`=== Object types in ${ontology} ===`);
  const types = await client.objects.listObjectTypes(ontology);
  for (const t of types) {
    console.log(`- ${t.apiName}\t${t.displayName}`);
  }
  if (types.length === 0) return;

  const objectType = types[0]!.apiName;
  console.log(`=== First 5 ${objectType} ===`);
  const customers = client.objects.of<CustomerRow>(ontology, objectType);
  const page = await customers.list({ pageSize: 5 });
  for (const row of page.data) {
    console.log(`- ${row['__primaryKey']}\t${JSON.stringify(row)}`);
  }

  // Action / Function / Subscribe demos are commented out — they require
  // ontology-specific names. Uncomment after generating fixtures.
  //
  //   await client.actions.apply(ontology, 'createOrder', { customerId: 'ALFKI' });
  //   const top = await client.functions.execute(ontology, 'topProducts', { limit: 10 });
  //   const sub = await client.subscribe.objects(ontology, { objectType });
  //   for await (const evt of sub) console.log(evt.state, evt.object['__primaryKey']);
}

main().catch((err: unknown) => {
  console.error(err instanceof Error ? err.message : err);
  process.exit(1);
});
