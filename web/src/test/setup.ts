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
