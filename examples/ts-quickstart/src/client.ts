// WeaveClient — top-level OSDK entry point. Owns one HttpTransport and
// exposes per-feature sub-clients:
//
//   const client = new WeaveClient({ baseUrl: 'http://localhost:9117', token });
//   const types = await client.objects.listObjectTypes('northwind');
//   const customers = client.objects.of<Customer>('northwind', 'Customer');
//   const result = await client.actions.apply('northwind', 'createOrder', { customerId: 'X' });
//   const value = await client.functions.execute('northwind', 'topProducts', { limit: 10 });
//   const sub = await client.subscribe.objects('northwind', { objectType: 'Customer' });

import type { Ontology, ListResponse } from './openapi.js';
import { ObjectsClient } from './objects.js';
import { ActionsClient } from './actions.js';
import { FunctionsClient } from './functions.js';
import { SubscribeClient } from './subscribe.js';
import { FetchTransport, type ClientOptions, type HttpTransport } from './transport.js';

export interface WeaveClientOptions extends ClientOptions {}

export class WeaveClient {
  readonly http: HttpTransport;
  readonly objects: ObjectsClient;
  readonly actions: ActionsClient;
  readonly functions: FunctionsClient;
  readonly subscribe: SubscribeClient;

  constructor(opts: WeaveClientOptions = {}) {
    this.http = opts.transport ?? new FetchTransport(opts);
    this.objects = new ObjectsClient(this.http);
    this.actions = new ActionsClient(this.http);
    this.functions = new FunctionsClient(this.http);
    const baseUrl = (opts.baseUrl ?? 'http://localhost:9117').replace(/\/+$/, '');
    this.subscribe = new SubscribeClient(baseUrl, opts.token);
  }

  async listOntologies(): Promise<Ontology[]> {
    const resp = await this.http.request<ListResponse<Ontology>>('/api/v2/ontologies');
    return resp.data;
  }
}
