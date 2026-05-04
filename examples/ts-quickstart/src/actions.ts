// ActionsClient — typed wrapper around `/api/v2/ontologies/{ontology}/actions/...`.
//
// Generic `apply<P>(name, params)` accepts a parameter shape so consumers can
// declare per-action parameter types alongside their domain models.

import type {
  ActionType,
  ApplyActionRequest,
  ApplyActionResponse,
  ApplyBatchRequest,
  ApplyBatchResponse,
  ListResponse,
} from './openapi.js';
import { encodePath, type HttpTransport } from './transport.js';

export interface ApplyOptions {
  returnEdits?: 'NONE' | 'CHANGES' | 'ALL';
  branch?: string;
}

export class ActionsClient {
  constructor(private readonly http: HttpTransport) {}

  async list(ontology: string): Promise<ActionType[]> {
    const resp = await this.http.request<ListResponse<ActionType>>(
      `/api/v2/ontologies/${encodePath(ontology)}/actionTypes`,
    );
    return resp.data;
  }

  async apply<P extends Record<string, unknown> = Record<string, unknown>>(
    ontology: string,
    actionApiName: string,
    parameters: P,
    opts: ApplyOptions = {},
  ): Promise<ApplyActionResponse> {
    const body: ApplyActionRequest = { parameters };
    if (opts.returnEdits) body.options = { returnEdits: opts.returnEdits };
    return this.http.request<ApplyActionResponse>(
      this.actionPath(ontology, actionApiName, 'apply'),
      {
        method: 'POST',
        body,
        query: opts.branch ? { branch: opts.branch } : undefined,
      },
    );
  }

  async applyBatch(
    ontology: string,
    actionApiName: string,
    requests: ApplyActionRequest[],
    opts: ApplyOptions = {},
  ): Promise<ApplyBatchResponse> {
    const body: ApplyBatchRequest = { requests };
    if (opts.returnEdits) body.options = { returnEdits: opts.returnEdits };
    return this.http.request<ApplyBatchResponse>(
      this.actionPath(ontology, actionApiName, 'applyBatch'),
      {
        method: 'POST',
        body,
        query: opts.branch ? { branch: opts.branch } : undefined,
      },
    );
  }

  private actionPath(ontology: string, action: string, verb: string): string {
    return (
      `/api/v2/ontologies/${encodePath(ontology)}` +
      `/actions/${encodePath(action)}/${verb}`
    );
  }
}
