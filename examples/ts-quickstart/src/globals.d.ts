// Minimal ambient declarations so the quickstart typechecks without
// `@types/node`. Production code should `npm install --save-dev @types/node`
// instead — this file is just to keep `tsc --noEmit` dep-free.
//
// Covers (a) the Node globals (process, console) used by main.ts and
// the OSDK runtime, and (b) the `node:test` / `node:assert/strict`
// modules used by the OSDK unit tests. The DOM lib provided by tsconfig
// already covers `fetch`, `WebSocket`, `URL`, `URLSearchParams`,
// `AbortController`, etc.

declare const process: {
  readonly env: Record<string, string | undefined>;
  exit(code?: number): never;
};

declare const console: {
  log(...args: unknown[]): void;
  error(...args: unknown[]): void;
  warn(...args: unknown[]): void;
};

// Just enough of `node:test` to run our own suites. The real types in
// @types/node are richer; this is intentionally minimal so adding a
// type dependency stays optional.
declare module 'node:test' {
  export interface TestContext {
    name: string;
  }
  export type TestFn = (t?: TestContext) => unknown | Promise<unknown>;
  export function test(name: string, fn: TestFn): Promise<void>;
  export function describe(name: string, fn: () => void | Promise<void>): void;
  export function it(name: string, fn: TestFn): void;
  export function before(fn: () => void | Promise<void>): void;
  export function after(fn: () => void | Promise<void>): void;
}

declare module 'node:assert/strict' {
  function assert(value: unknown, message?: string): asserts value;
  namespace assert {
    function equal<T>(actual: T, expected: T, message?: string): void;
    function deepEqual<T>(actual: T, expected: T, message?: string): void;
    function notEqual<T>(actual: T, expected: T, message?: string): void;
    function ok(value: unknown, message?: string): asserts value;
    function rejects(
      promise: Promise<unknown> | (() => Promise<unknown>),
      error?: RegExp | ((err: unknown) => boolean) | object,
      message?: string,
    ): Promise<void>;
    function throws(
      block: () => unknown,
      error?: RegExp | ((err: unknown) => boolean) | object,
      message?: string,
    ): void;
    function fail(message?: string): never;
    function match(actual: string, expected: RegExp, message?: string): void;
  }
  export default assert;
}
