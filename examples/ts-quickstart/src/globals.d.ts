// Minimal ambient declarations so the quickstart typechecks without
// `@types/node`. Production code should `npm install --save-dev @types/node`
// instead — this file is just to keep `tsc --noEmit` dep-free.

declare const process: {
  readonly env: Record<string, string | undefined>;
  exit(code?: number): never;
};

declare const console: {
  log(...args: unknown[]): void;
  error(...args: unknown[]): void;
};
