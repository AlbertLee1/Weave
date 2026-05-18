import { describe, expect, it } from 'vitest';
import serviceWorkerSource from '../serviceWorker.ts?raw';

describe('BDD: service worker offline replay chunking (SELF-410)', () => {
  it('Given the replay bridge imports the queue statically, When source is checked, Then it does not dynamically import the same module', () => {
    expect(serviceWorkerSource).toMatch(/from ['"]\.\/offlineRequestQueue['"]/);
    expect(serviceWorkerSource).not.toContain("import('./offlineRequestQueue')");
    expect(serviceWorkerSource).not.toContain('import("./offlineRequestQueue")');
  });
});
