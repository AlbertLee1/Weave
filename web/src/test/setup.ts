import '@testing-library/jest-dom/vitest'

// jsdom does not implement ResizeObserver / Element.scrollIntoView; cmdk's
// <Command.Input> uses both on mount. Polyfill with no-ops so component
// tests can render <CommandPalette> without blowing up.
if (typeof globalThis.ResizeObserver === 'undefined') {
  class ResizeObserverStub {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver;
}
if (typeof Element !== 'undefined' && !Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = function scrollIntoView() {};
}
// US-456: uplot reads window.matchMedia at module-import time to detect
// device pixel ratio. jsdom does not ship matchMedia, so any test that
// transitively imports uplot blows up on require. The stub returns a
// listener-shaped object that satisfies the addEventListener interface.
if (typeof globalThis.matchMedia === 'undefined') {
  globalThis.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    dispatchEvent() {
      return false;
    },
  })) as unknown as typeof globalThis.matchMedia;
}
