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

// US-455: POST a new commit to the per-Function bare git repo. SourceCode
// is the new file body; Message is required. The handler resolves the ref
// by RID / name / name@version so the SPA can pass either.
export interface CreateFunctionRepoCommitRequest {
  message: string;
  sourceCode: string;
  author?: string;
  email?: string;
}

export function createFunctionRepoCommit(
  ontologyApiName: string,
  functionRid: string,
  body: CreateFunctionRepoCommitRequest,
): Promise<FunctionRepoCommit> {
  return request<FunctionRepoCommit>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/functions/${encodeURIComponent(functionRid)}/commits`,
    body,
  );
}

// US-417: per-commit CI job. The status field drives the ✅/❌ badge in the
// FunctionDiff UI; the per-phase output strings power the tooltip / drawer
// the operator opens to see the raw lint / test logs.
export type CommitJobStatus =
  | 'queued'
  | 'running'
  | 'success'
  | 'failure'
  | 'skipped';

export interface CommitJob {
  id: number;
  functionRid: string;
  commitSha: string;
  status: CommitJobStatus;
  lintOutput?: string;
  testOutput?: string;
  errorMessage?: string;
  startedAt?: string;
  finishedAt?: string;
  createdAt: string;
  updatedAt: string;
}

// getFunctionCommitJob returns null when the server reports 404
// CommitJobNotFound (commit was not picked up by the hook) — call sites
// can render an "no CI run" empty state without wrapping in try/catch.
export async function getFunctionCommitJob(
  ontologyApiName: string,
  functionRid: string,
  hash: string,
): Promise<CommitJob | null> {
  try {
    return await request<CommitJob>(
      'GET',
      `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/functions/${encodeURIComponent(functionRid)}/commits/${encodeURIComponent(hash)}/job`,
    );
  } catch (err) {
    const status = (err as { statusCode?: number })?.statusCode;
    if (status === 404 || status === 503) {
      return null;
    }
    throw err;
  }
}

// US-046: Versions list for the Function code-repository switcher.
// Mirrors `oms.Function` (pkg/oms/models.go) — the wire shape returned
// by GET /functions/{functionName}/versions is `{ name, data: [...] }`
// with the rows sorted latest-first by the backend.
export interface FunctionVersion {
  rid: string;
  name: string;
  version: string;
  sourceCode: string;
  runtime?: string;
  pure?: boolean;
  createdBy?: string;
  createdAt?: string;
  codeHash?: string;
  signatureHash?: string;
  publishedAt?: string;
}

export interface FunctionVersionsResponse {
  name: string;
  data: FunctionVersion[];
}

export function listFunctionVersions(
  ontologyApiName: string,
  functionName: string,
): Promise<FunctionVersionsResponse> {
  return request<FunctionVersionsResponse>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/functions/${encodeURIComponent(functionName)}/versions`,
  );
}

// US-046: Replay endpoint wire shape (pkg/oms/handlers_function_replay.go,
// US-370). At minimum the caller passes `input` (parameter map) and an
// optional `version` pin; passing `executionId` instead replays a stored
// historical invocation by id. The server returns the fresh result plus
// determinism metadata (`match`, `originalHash`, `replayHash`).
export interface ReplayFunctionRequest {
  executionId?: string;
  version?: string;
  input?: Record<string, unknown>;
}

export interface ReplayFunctionWarning {
  code: string;
  message: string;
}

export interface ReplayFunctionResponse {
  functionRid: string;
  functionVersion: string;
  executionId?: string;
  originalHash?: string;
  replayHash: string;
  match: boolean;
  result: unknown;
  warning?: ReplayFunctionWarning;
  original?: unknown;
}

export function replayFunction(
  ontologyApiName: string,
  functionRid: string,
  body: ReplayFunctionRequest,
): Promise<ReplayFunctionResponse> {
  return request<ReplayFunctionResponse>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/functions/${encodeURIComponent(functionRid)}/replay`,
    body,
  );
}
