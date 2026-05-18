const packageChunkNames = new Map<string, string>([
  ['react', 'vendor-react'],
  ['react-dom', 'vendor-react'],
  ['react-error-boundary', 'vendor-react'],
  ['react-router', 'vendor-react'],
  ['scheduler', 'vendor-react'],
  ['@tanstack/query-core', 'vendor-react'],
  ['@tanstack/react-query', 'vendor-react'],
  ['use-sync-external-store', 'vendor-react'],
  ['zustand', 'vendor-react'],

  ['@react-sigma/core', 'vendor-graph'],
  ['@xyflow/react', 'vendor-graph'],
  ['@xyflow/system', 'vendor-graph'],
  ['@dagrejs/dagre', 'vendor-graph'],
  ['d3-color', 'vendor-graph'],
  ['d3-dispatch', 'vendor-graph'],
  ['d3-drag', 'vendor-graph'],
  ['d3-ease', 'vendor-graph'],
  ['d3-force', 'vendor-graph'],
  ['d3-interpolate', 'vendor-graph'],
  ['d3-quadtree', 'vendor-graph'],
  ['d3-selection', 'vendor-graph'],
  ['d3-timer', 'vendor-graph'],
  ['d3-transition', 'vendor-graph'],
  ['d3-zoom', 'vendor-graph'],
  ['graphology', 'vendor-graph'],
  ['graphology-layout', 'vendor-graph'],
  ['graphology-layout-forceatlas2', 'vendor-graph'],
  ['graphology-utils', 'vendor-graph'],
  ['sigma', 'vendor-graph'],

  ['@uiw/copy-to-clipboard', 'vendor-markdown'],
  ['@uiw/react-markdown-preview', 'vendor-markdown'],
  ['@uiw/react-md-editor', 'vendor-markdown'],
  ['@uiw/react-textarea-code-editor', 'vendor-markdown'],
  ['boolbase', 'vendor-markdown'],
  ['character-entities-html4', 'vendor-markdown'],
  ['character-reference-invalid', 'vendor-markdown'],
  ['classcat', 'vendor-markdown'],
  ['css-selector-parser', 'vendor-markdown'],
  ['direction', 'vendor-markdown'],
  ['entities', 'vendor-markdown'],
  ['estree-util-is-identifier-name', 'vendor-markdown'],
  ['extend', 'vendor-markdown'],
  ['github-slugger', 'vendor-markdown'],
  ['hastscript', 'vendor-markdown'],
  ['html-parse-stringify', 'vendor-markdown'],
  ['html-void-elements', 'vendor-markdown'],
  ['is-alphabetical', 'vendor-markdown'],
  ['is-alphanumerical', 'vendor-markdown'],
  ['is-decimal', 'vendor-markdown'],
  ['is-hexadecimal', 'vendor-markdown'],
  ['is-plain-obj', 'vendor-markdown'],
  ['longest-streak', 'vendor-markdown'],
  ['markdown-table', 'vendor-markdown'],
  ['parse-entities', 'vendor-markdown'],
  ['parse-numeric-range', 'vendor-markdown'],
  ['parse5', 'vendor-markdown'],
  ['react-markdown', 'vendor-markdown'],
  ['refractor', 'vendor-markdown'],
  ['rehype', 'vendor-markdown'],
  ['rehype-attr', 'vendor-markdown'],
  ['rehype-autolink-headings', 'vendor-markdown'],
  ['rehype-ignore', 'vendor-markdown'],
  ['rehype-parse', 'vendor-markdown'],
  ['rehype-prism-plus', 'vendor-markdown'],
  ['rehype-raw', 'vendor-markdown'],
  ['rehype-rewrite', 'vendor-markdown'],
  ['rehype-slug', 'vendor-markdown'],
  ['rehype-stringify', 'vendor-markdown'],
  ['stringify-entities', 'vendor-markdown'],
  ['style-to-js', 'vendor-markdown'],
  ['style-to-object', 'vendor-markdown'],
  ['stylis', 'vendor-markdown'],
  ['trough', 'vendor-markdown'],
  ['remark-github-blockquote-alert', 'vendor-markdown'],
  ['remark-gfm', 'vendor-markdown'],
  ['remark-parse', 'vendor-markdown'],
  ['remark-rehype', 'vendor-markdown'],
  ['remark-stringify', 'vendor-markdown'],
  ['unified', 'vendor-markdown'],
  ['vfile', 'vendor-markdown'],
  ['vfile-location', 'vendor-markdown'],
  ['vfile-message', 'vendor-markdown'],
  ['void-elements', 'vendor-markdown'],
  ['web-namespaces', 'vendor-markdown'],

  ['uplot', 'vendor-charts'],

  ['adler-32', 'vendor-spreadsheet'],
  ['cfb', 'vendor-spreadsheet'],
  ['codepage', 'vendor-spreadsheet'],
  ['crc-32', 'vendor-spreadsheet'],
  ['ssf', 'vendor-spreadsheet'],
  ['wmf', 'vendor-spreadsheet'],
  ['word', 'vendor-spreadsheet'],
  ['xlsx', 'vendor-spreadsheet'],

  ['@react-leaflet/core', 'vendor-maps'],
  ['leaflet', 'vendor-maps'],
  ['react-leaflet', 'vendor-maps'],

  ['isomorphic.js', 'vendor-collab'],
  ['lib0', 'vendor-collab'],
  ['y-protocols', 'vendor-collab'],
  ['y-websocket', 'vendor-collab'],
  ['yjs', 'vendor-collab'],

  ['i18next', 'vendor-i18n'],
  ['react-i18next', 'vendor-i18n'],

  ['@hookform/resolvers', 'vendor-forms'],
  ['react-hook-form', 'vendor-forms'],
  ['zod', 'vendor-forms'],

  ['js-yaml', 'vendor-data'],
  ['yaml', 'vendor-data'],

  ['diff', 'vendor-diff'],
  ['react-diff-viewer-continued', 'vendor-diff'],

  ['file-selector', 'vendor-files'],
  ['attr-accept', 'vendor-files'],
  ['react-dropzone', 'vendor-files'],

  ['cmdk', 'vendor-ui'],
  ['aria-hidden', 'vendor-ui'],
  ['classnames', 'vendor-ui'],
  ['memoize-one', 'vendor-ui'],
  ['prop-types', 'vendor-ui'],
  ['react-hotkeys-hook', 'vendor-ui'],
  ['react-remove-scroll', 'vendor-ui'],
  ['react-remove-scroll-bar', 'vendor-ui'],
  ['react-style-singleton', 'vendor-ui'],
  ['use-callback-ref', 'vendor-ui'],
  ['use-sidecar', 'vendor-ui'],
]);

const markdownPrefixes = [
  '@emotion/',
  '@mdx-js/',
  'bail',
  'bcp-47',
  'ccount',
  'character-entities',
  'comma-separated-tokens',
  'decode-named-character-reference',
  'devlop',
  'hast-util-',
  'html-url-attributes',
  'inline-style-parser',
  'mdast-util-',
  'micromark',
  'nth-check',
  'property-information',
  'space-separated-tokens',
  'trim-lines',
  'unist-util-',
  'zwitch',
];

const uiPrefixes = ['@radix-ui/'];

export function packageNameForModuleId(id: string): string | null {
  const normalized = id.replaceAll('\\', '/');
  const marker = '/node_modules/';
  const nodeModulesIndex = normalized.lastIndexOf(marker);
  if (nodeModulesIndex < 0) return null;

  const packagePath = normalized.slice(nodeModulesIndex + marker.length);
  const parts = packagePath.split('/');
  if (parts[0] === '.pnpm' && parts.length >= 4) {
    return parts[2]?.startsWith('@') ? `${parts[2]}/${parts[3]}` : parts[2] ?? null;
  }
  if (parts[0]?.startsWith('@')) {
    return parts.length >= 2 ? `${parts[0]}/${parts[1]}` : null;
  }
  return parts[0] || null;
}

export function chunkNameForModule(id: string): string | undefined {
  const packageName = packageNameForModuleId(id);
  if (!packageName) return undefined;

  const configured = packageChunkNames.get(packageName);
  if (configured) return configured;
  if (markdownPrefixes.some((prefix) => packageName.startsWith(prefix))) {
    return 'vendor-markdown';
  }
  if (uiPrefixes.some((prefix) => packageName.startsWith(prefix))) {
    return 'vendor-ui';
  }
  return 'vendor-misc';
}
