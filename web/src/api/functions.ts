import { request } from './client';

// US-415 / US-416: wire shapes for the Function code-repository surface.
// Mirrors the JSON returned by the OMS handlers so the diff UI can stay
// independent of go-git internals — fields here are the cross-language
// stable subset (no committer email, no parent hash).
export interface FunctionRepoCommit {
  hash: string;
  message: string;
  author: string;
  email: string;
  authorDate: string;
}

export interface FunctionRepoCommitWithSource extends FunctionRepoCommit {
  sourceCode: string;
}

export interface FunctionRepoCommitsResponse {
  data: FunctionRepoCommit[];
}

// FunctionSummary is a slim projection of `oms.Function` — the diff UI's
// commit picker only renders the human name + canonical RID + current
// `sourceCode` (used as the "working tree" left-hand side when comparing
// against the most recent committed revision).
export interface FunctionSummary {
  rid: string;
  ontologyRid: string;
  name: string;
  version?: string;
  sourceCode: string;
  runtime?: string;
}

export function listFunctionRepoCommits(
  ontologyApiName: string,
  functionRid: string,
  limit?: number,
): Promise<FunctionRepoCommitsResponse> {
  const limitSuffix = limit && limit > 0 ? `?limit=${limit}` : '';
  return request<FunctionRepoCommitsResponse>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/functions/${encodeURIComponent(functionRid)}/log${limitSuffix}`,
  );
}

export function getFunctionRepoCommit(
  ontologyApiName: string,
  functionRid: string,
  hash: string,
): Promise<FunctionRepoCommitWithSource> {
  return request<FunctionRepoCommitWithSource>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/functions/${encodeURIComponent(functionRid)}/commits/${encodeURIComponent(hash)}`,
  );
}

export function getFunction(
  ontologyApiName: string,
  functionRid: string,
): Promise<FunctionSummary> {
  return request<FunctionSummary>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/functions/${encodeURIComponent(functionRid)}`,
  );
}
