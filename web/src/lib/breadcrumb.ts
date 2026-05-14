// Common acronyms preserved in their uppercase form when they appear as a
// standalone segment word (e.g. `saga-dlq` -> `Saga DLQ`). Add entries here
// rather than special-casing call sites.
const ABBR = new Set(['DLQ', 'ID', 'API', 'URL', 'TS']);

// splitCamelCase converts a route-segment slug into a human-friendly label:
//   - kebab-case becomes spaced ('saga-dlq' -> 'Saga DLQ')
//   - camelCase splits at the lower->upper transition ('objectTypes' -> 'Object Types')
//   - each word is title-cased, with acronyms in ABBR kept uppercase
//   - inputs without any transition fall back to a single Title-cased word
export function splitCamelCase(s: string): string {
  return s
    .replace(/-/g, ' ')
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .split(/\s+/)
    .filter(Boolean)
    .map((w) =>
      ABBR.has(w.toUpperCase())
        ? w.toUpperCase()
        : w[0].toUpperCase() + w.slice(1),
    )
    .join(' ');
}
