import { request } from './client';

// US-047 (PC-A04): Interface Methods Console.
//
// Mirrors `pkg/oms.InterfaceMethod` plus the
// InvokeInterfaceMethod{Request,Response} envelope exposed by
// pkg/oms/admin_handlers_interface_method.go. Used by the per-object
// "Interface Methods" console reachable from the Browser detail panel.

export interface InterfaceMethodParam {
  name: string;
  type: string;
  required?: boolean;
  default?: unknown;
}

export interface InterfaceMethodReturns {
  type: string;
}

export interface InterfaceMethod {
  rid: string;
  interfaceRid: string;
  name: string;
  params: InterfaceMethodParam[];
  returns: InterfaceMethodReturns;
  description?: string;
}

export interface InterfaceMethodsListResponse {
  data: InterfaceMethod[];
}

export interface InvokeInterfaceMethodRequest {
  objectType: string;
  parameters?: Record<string, unknown>;
}

// Honest mapping: the backend wire field is `actionTypeApiName` and
// `actionTypeRid` — the per-method dispatch resolves to a concrete
// ActionType (US-214). The `result` payload is opaque JSON the backend
// forwards from the underlying Executor when one is wired; without a
// dispatcher it is omitted (resolution-only mode).
export interface InvokeInterfaceMethodResponse {
  actionTypeRid: string;
  actionTypeApiName: string;
  objectType: string;
  methodRid: string;
  result?: unknown;
}

export function listInterfaceMethods(
  ontologyApiName: string,
  interfaceRid: string,
): Promise<InterfaceMethodsListResponse> {
  return request<InterfaceMethodsListResponse>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/interfaces/${encodeURIComponent(interfaceRid)}/methods`,
  );
}

// The backend path is `/interfaces/methods/{methodRid}/invoke` (not
// `/execute` as the PRD AC phrased it). pkg/oms/admin_handlers_interface_method.go
// is authoritative — same handler resolves the concrete ActionType +
// dispatches via the optional InterfaceMethodActionDispatcher.
export function invokeInterfaceMethod(
  ontologyApiName: string,
  methodRid: string,
  body: InvokeInterfaceMethodRequest,
): Promise<InvokeInterfaceMethodResponse> {
  return request<InvokeInterfaceMethodResponse>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/interfaces/methods/${encodeURIComponent(methodRid)}/invoke`,
    body,
  );
}
