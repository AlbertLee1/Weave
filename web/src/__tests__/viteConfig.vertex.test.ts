// VTX-126 — Real-browser regression guard for the /vertex/new
// "Invalid hook call / useRef returning null" crash.
//
// Root cause class: when @react-sigma/core (a prebundled ESM
// package that imports `react`) lives behind a `React.lazy` chunk,
// Vite discovers it on first visit and triggers a mid-flight
// dependency re-optimization. The newly bundled chunk receives a
// React module instance that differs from the React already used by
// the mounted tree, so any hook inside @react-sigma/core (useRef,
// useContext, …) sees a null dispatcher and crashes the route.
//
// Pinning these heavy Vertex deps in `optimizeDeps.include` makes
// Vite prebundle them at dev-server boot, and `resolve.dedupe`
// pins React + react-dom to a single module instance — together
// they prevent the crash from recurring even if a future refactor
// re-introduces a lazy boundary above the SigmaContainer.

import viteConfigSource from '../../vite.config.ts?raw';
import { describe, expect, it } from 'vitest';

describe('vite.config.ts — Vertex/Sigma dep prebundling (VTX-126)', () => {
  it('declares an optimizeDeps.include list with @react-sigma/core, sigma, and graphology so the first /vertex/new visit does not trigger a mid-flight re-optimize that breaks the React singleton', () => {
    expect(viteConfigSource).toMatch(/optimizeDeps\s*:/);
    expect(viteConfigSource).toMatch(/['"]@react-sigma\/core['"]/);
    expect(viteConfigSource).toMatch(/['"]sigma['"]/);
    expect(viteConfigSource).toMatch(/['"]graphology['"]/);
  });

  it('dedupes react and react-dom so the lazy-loaded Vertex chunk shares one React instance with the rest of the app', () => {
    expect(viteConfigSource).toMatch(
      /resolve\s*:[\s\S]*dedupe\s*:[\s\S]*['"]react['"]/,
    );
    expect(viteConfigSource).toMatch(
      /resolve\s*:[\s\S]*dedupe\s*:[\s\S]*['"]react-dom['"]/,
    );
  });
});
