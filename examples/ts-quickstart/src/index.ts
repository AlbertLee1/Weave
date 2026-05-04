// Public OSDK barrel — import what you need from `weave-ts-quickstart`.
//
//   import { WeaveClient } from 'weave-ts-quickstart';
//   import type { ObjectRow, ChangeEvent } from 'weave-ts-quickstart';

export { WeaveClient } from './client.js';
export type { WeaveClientOptions } from './client.js';

export { ObjectsClient, ObjectTypeClient } from './objects.js';
export type { ListOptions, SearchOptions, LinkedObjectsOptions } from './objects.js';

export { ActionsClient } from './actions.js';
export type { ApplyOptions } from './actions.js';

export { FunctionsClient } from './functions.js';
export type { ExecuteOptions } from './functions.js';

export { SubscribeClient, Subscription, WeaveOutOfDateError } from './subscribe.js';
export type {
  SubscribeOptions,
  SubscribeTransport,
  TransportFactory,
  ChangeEvent,
} from './subscribe.js';

export { FetchTransport, WeaveHttpError } from './transport.js';
export type { HttpTransport, RequestOptions, ClientOptions } from './transport.js';

export type {
  Ontology,
  ObjectType,
  ObjectRow,
  ObjectPage,
  LinkType,
  ActionType,
  ActionParameter,
  ApplyActionRequest,
  ApplyActionResponse,
  ApplyBatchRequest,
  ApplyBatchResponse,
  ExecuteFunctionRequest,
  ExecuteFunctionResponse,
  FunctionDefinition,
  ListResponse,
  ObjectChangeEvent,
  ChangeState,
  SubscribeMessage,
  SubscribeRequestPayload,
  PropertyDefinition,
  APIError,
} from './openapi.js';
