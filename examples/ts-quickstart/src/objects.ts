// ObjectsClient — typed wrapper around `/api/v2/ontologies/{ontology}/objects/...`
// surfaces.
//
// Generic `T extends ObjectRow` makes consumer code self-documenting:
//
//   const customers = client.objects.of<Customer>('northwind', 'Customer');
//   const page = await customers.list({ pageSize: 10 });

import type {
  ListResponse,
  ObjectPage,
  ObjectRow,
  ObjectType,
} from './openapi.js';
import { encodePath, type HttpTransport } from './transport.js';

export interface ListOptions {
  pageSize?: number;
  pageToken?: string;
  orderBy?: string;
  select?: string[];
  branch?: string;
}

export interface SearchOptions extends ListOptions {
  where?: Record<string, unknown>;
}

export interface LinkedObjectsOptions extends ListOptions {}

export class ObjectTypeClient<T extends ObjectRow = ObjectRow> {
  constructor(
    private readonly http: HttpTransport,
    readonly ontologyApiName: string,
    readonly objectTypeApiName: string,
  ) {}

  async list(opts: ListOptions = {}): Promise<ObjectPage> {
    return this.http.request<ObjectPage>(this.basePath(), {
      query: this.listQuery(opts),
    });
  }

  async get(primaryKey: string, opts: { branch?: string } = {}): Promise<T> {
    return this.http.request<T>(
      `${this.basePath()}/${encodePath(primaryKey)}`,
      { query: opts.branch ? { branch: opts.branch } : undefined },
    );
  }

  async search(opts: SearchOptions): Promise<ObjectPage> {
    const body: Record<string, unknown> = {};
    if (opts.where !== undefined) body['where'] = opts.where;
    if (opts.pageSize !== undefined) body['pageSize'] = opts.pageSize;
    if (opts.pageToken !== undefined) body['pageToken'] = opts.pageToken;
    if (opts.orderBy !== undefined) body['orderBy'] = opts.orderBy;
    if (opts.select !== undefined) body['select'] = opts.select;
    return this.http.request<ObjectPage>(`${this.basePath()}/search`, {
      method: 'POST',
      body,
      query: opts.branch ? { branch: opts.branch } : undefined,
    });
  }

  async linkedObjects(
    primaryKey: string,
    linkApiName: string,
    opts: LinkedObjectsOptions = {},
  ): Promise<ObjectPage> {
    return this.http.request<ObjectPage>(
      `${this.basePath()}/${encodePath(primaryKey)}/links/${encodePath(linkApiName)}`,
      { query: this.listQuery(opts) },
    );
  }

  // Async iterator that walks every page. Caller controls `pageSize`;
  // the iterator stops when `nextPageToken` is undefined or empty.
  async *iterate(opts: ListOptions = {}): AsyncIterableIterator<ObjectRow> {
    let token: string | undefined = opts.pageToken;
    while (true) {
      const page: ObjectPage = await this.list({ ...opts, pageToken: token });
      for (const row of page.data) yield row;
      if (!page.nextPageToken) return;
      token = page.nextPageToken;
    }
  }

  private basePath(): string {
    return (
      `/api/v2/ontologies/${encodePath(this.ontologyApiName)}` +
      `/objects/${encodePath(this.objectTypeApiName)}`
    );
  }

  private listQuery(opts: ListOptions): Record<string, string | number | undefined> {
    const q: Record<string, string | number | undefined> = {};
    if (opts.pageSize !== undefined) q['pageSize'] = opts.pageSize;
    if (opts.pageToken !== undefined) q['pageToken'] = opts.pageToken;
    if (opts.orderBy !== undefined) q['orderBy'] = opts.orderBy;
    if (opts.select && opts.select.length > 0) q['select'] = opts.select.join(',');
    if (opts.branch) q['branch'] = opts.branch;
    return q;
  }
}

// ObjectsClient is the per-ontology entry point. `of<T>(ontology, type)`
// returns a typed `ObjectTypeClient<T>`; metadata methods give untyped
// listings (object types, primary keys) for discovery.
export class ObjectsClient {
  constructor(private readonly http: HttpTransport) {}

  of<T extends ObjectRow = ObjectRow>(
    ontology: string,
    objectType: string,
  ): ObjectTypeClient<T> {
    return new ObjectTypeClient<T>(this.http, ontology, objectType);
  }

  async listObjectTypes(ontology: string): Promise<ObjectType[]> {
    const resp = await this.http.request<ListResponse<ObjectType>>(
      `/api/v2/ontologies/${encodePath(ontology)}/objectTypes`,
    );
    return resp.data;
  }
}
