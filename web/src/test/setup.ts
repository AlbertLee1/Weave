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

// jsdom intentionally leaves canvas drawing and window.open unimplemented.
// Several upper-layer pages import charting, graph, or export helpers during
// tests; quiet, deterministic shims keep the CI log focused on real failures.
const canvasContextCache = new WeakMap<HTMLCanvasElement, CanvasRenderingContext2D>();

function createCanvasGradientStub(): CanvasGradient {
  return {
    addColorStop() {},
  } as CanvasGradient;
}

function createCanvasPatternStub(): CanvasPattern {
  return {
    setTransform() {},
  } as CanvasPattern;
}

function createCanvas2DContext(canvas: HTMLCanvasElement): CanvasRenderingContext2D {
  const noop = () => {};
  const ctx = {
    canvas,
    fillStyle: '#000000',
    strokeStyle: '#000000',
    font: '10px sans-serif',
    globalAlpha: 1,
    globalCompositeOperation: 'source-over',
    imageSmoothingEnabled: true,
    imageSmoothingQuality: 'low',
    lineCap: 'butt',
    lineDashOffset: 0,
    lineJoin: 'miter',
    lineWidth: 1,
    miterLimit: 10,
    shadowBlur: 0,
    shadowColor: 'rgba(0, 0, 0, 0)',
    shadowOffsetX: 0,
    shadowOffsetY: 0,
    textAlign: 'start',
    textBaseline: 'alphabetic',
    direction: 'inherit',
    arc: noop,
    arcTo: noop,
    beginPath: noop,
    bezierCurveTo: noop,
    clearRect: noop,
    clip: noop,
    closePath: noop,
    drawFocusIfNeeded: noop,
    drawImage: noop,
    ellipse: noop,
    fill: noop,
    fillRect: noop,
    fillText: noop,
    lineTo: noop,
    moveTo: noop,
    putImageData: noop,
    quadraticCurveTo: noop,
    rect: noop,
    reset: noop,
    resetTransform: noop,
    restore: noop,
    rotate: noop,
    roundRect: noop,
    save: noop,
    scale: noop,
    scrollPathIntoView: noop,
    setLineDash: noop,
    setTransform: noop,
    stroke: noop,
    strokeRect: noop,
    strokeText: noop,
    transform: noop,
    translate: noop,
    createConicGradient: createCanvasGradientStub,
    createImageData: (_width: number, _height: number) =>
      new ImageData(Math.max(1, _width), Math.max(1, _height)),
    createLinearGradient: createCanvasGradientStub,
    createPattern: createCanvasPatternStub,
    createRadialGradient: createCanvasGradientStub,
    getContextAttributes: () => ({
      alpha: true,
      colorSpace: 'srgb',
      desynchronized: false,
      willReadFrequently: false,
    }),
    getImageData: (_sx: number, _sy: number, _sw: number, _sh: number) =>
      new ImageData(Math.max(1, _sw), Math.max(1, _sh)),
    getLineDash: () => [],
    isContextLost: () => false,
    isPointInPath: () => false,
    isPointInStroke: () => false,
    measureText: (text: string) =>
      ({
        width: Math.max(1, text.length * 8),
        actualBoundingBoxAscent: 8,
        actualBoundingBoxDescent: 2,
        actualBoundingBoxLeft: 0,
        actualBoundingBoxRight: Math.max(1, text.length * 8),
        fontBoundingBoxAscent: 8,
        fontBoundingBoxDescent: 2,
      }) as TextMetrics,
  } as unknown as CanvasRenderingContext2D;
  return ctx;
}

if (typeof HTMLCanvasElement !== 'undefined') {
  Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', {
    configurable: true,
    value(this: HTMLCanvasElement, contextId: string) {
      if (contextId !== '2d') return null;
      let ctx = canvasContextCache.get(this);
      if (!ctx) {
        ctx = createCanvas2DContext(this);
        canvasContextCache.set(this, ctx);
      }
      return ctx;
    },
  });
}

if (typeof globalThis.Path2D === 'undefined') {
  class Path2DStub {
    constructor(_path?: Path2D | string) {}
    addPath(_path: Path2D, _transform?: DOMMatrix2DInit) {}
    arc(
      _x: number,
      _y: number,
      _radius: number,
      _startAngle: number,
      _endAngle: number,
      _counterclockwise?: boolean,
    ) {}
    arcTo(
      _x1: number,
      _y1: number,
      _x2: number,
      _y2: number,
      _radius: number,
    ) {}
    bezierCurveTo(
      _cp1x: number,
      _cp1y: number,
      _cp2x: number,
      _cp2y: number,
      _x: number,
      _y: number,
    ) {}
    closePath() {}
    ellipse(
      _x: number,
      _y: number,
      _radiusX: number,
      _radiusY: number,
      _rotation: number,
      _startAngle: number,
      _endAngle: number,
      _counterclockwise?: boolean,
    ) {}
    lineTo(_x: number, _y: number) {}
    moveTo(_x: number, _y: number) {}
    quadraticCurveTo(_cpx: number, _cpy: number, _x: number, _y: number) {}
    rect(_x: number, _y: number, _w: number, _h: number) {}
    roundRect(
      _x: number,
      _y: number,
      _w: number,
      _h: number,
      _radii?: number | DOMPointInit | (number | DOMPointInit)[],
    ) {}
  }
  Object.defineProperty(globalThis, 'Path2D', {
    configurable: true,
    writable: true,
    value: Path2DStub,
  });
  if (typeof window !== 'undefined') {
    Object.defineProperty(window, 'Path2D', {
      configurable: true,
      writable: true,
      value: Path2DStub,
    });
  }
}

if (typeof window !== 'undefined') {
  Object.defineProperty(window, 'open', {
    configurable: true,
    writable: true,
    value: () => null,
  });
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
