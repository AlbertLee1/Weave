// FunctionsClient — typed wrapper around
// `/api/v2/ontologies/{ontology}/functions/{ref}/execute`.
//
// `ref` accepts a Function RID, a bare name, or `name@version` (semver pin).

import type {
  ExecuteFunctionRequest,
  ExecuteFunctionResponse,
  FunctionDefinition,
  ListResponse,
} from './openapi.js';
import { encodePath, type HttpTransport } from './transport.js';

export interface ExecuteOptions {
  branch?: string;
}

export class FunctionsClient {
  constructor(private readonly http: HttpTransport) {}

  async list(ontology: string): Promise<FunctionDefinition[]> {
    const resp = await this.http.request<ListResponse<FunctionDefinition>>(
      `/api/v2/ontologies/${encodePath(ontology)}/functions`,
    );
    return resp.data;
  }

  async execute<R = unknown, P extends Record<string, unknown> = Record<string, unknown>>(
    ontology: string,
    ref: string,
    parameters: P,
    opts: ExecuteOptions = {},
  ): Promise<R> {
    const body: ExecuteFunctionRequest = { parameters };
    const resp = await this.http.request<ExecuteFunctionResponse | R>(
      `/api/v2/ontologies/${encodePath(ontology)}/functions/${encodePath(ref)}/execute`,
      {
        method: 'POST',
        body,
        query: opts.branch ? { branch: opts.branch } : undefined,
      },
    );
    if (
      resp !== null &&
      typeof resp === 'object' &&
      'result' in (resp as object)
    ) {
      return (resp as ExecuteFunctionResponse).result as R;
    }
    return resp as R;
  }
}
