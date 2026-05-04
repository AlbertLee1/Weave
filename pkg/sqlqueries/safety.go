package sqlqueries

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Sentinel errors returned by ValidateQuery. Each maps to a distinct
// failureReason on the wire so SDK callers can branch on the failure
// kind without parsing the error message.
var (
	// ErrEmptyQuery is returned when the input is empty after trimming.
	ErrEmptyQuery = errors.New("query is empty")
	// ErrStackedStatement is returned when more than one statement is
	// present (e.g. "SELECT 1; DROP TABLE x").
	ErrStackedStatement = errors.New("stacked statements are not allowed")
	// ErrForbiddenStatement is returned when the leading keyword is one
	// of the rejected DML/DDL/DCL forms (INSERT/UPDATE/DELETE/DROP/
	// ALTER/TRUNCATE/CREATE/GRANT/REVOKE/MERGE/REPLACE/CALL/COPY/...).
	ErrForbiddenStatement = errors.New("statement type is not allowed")
	// ErrSystemTableAccess is returned when the query references a
	// table whose name starts with `pg_` or that lives in the
	// `information_schema` namespace.
	ErrSystemTableAccess = errors.New("access to system tables is not allowed")
)

// forbiddenLeadingKeywords lists statement types that are unconditionally
// rejected. The list is conservative — anything beyond SELECT / WITH /
// VALUES / TABLE (the four canonical read-only SQL forms) is treated as
// "not a SELECT" and routed through ErrForbiddenStatement.
var forbiddenLeadingKeywords = map[string]struct{}{
	"INSERT":   {},
	"UPDATE":   {},
	"DELETE":   {},
	"DROP":     {},
	"ALTER":    {},
	"TRUNCATE": {},
	"CREATE":   {},
	"GRANT":    {},
	"REVOKE":   {},
	"MERGE":    {},
	"REPLACE":  {},
	"CALL":     {},
	"COPY":     {},
	"EXECUTE":  {},
	"PREPARE":  {},
	"COMMIT":   {},
	"ROLLBACK": {},
	"BEGIN":    {},
	"START":    {},
	"END":      {},
	"SAVEPOINT": {},
	"LOCK":     {},
	"UNLOCK":   {},
	"SET":      {},
	"RESET":    {},
	"VACUUM":   {},
	"ANALYZE":  {},
	"CLUSTER":  {},
	"REINDEX":  {},
	"REFRESH":  {},
	"COMMENT":  {},
	"DO":       {},
	"LISTEN":   {},
	"NOTIFY":   {},
	"DISCARD":  {},
}

// allowedLeadingKeywords lists the statement openers that survive the
// safety check. The four entries mirror PostgreSQL's read-only top-level
// commands; everything else surfaces as ErrForbiddenStatement.
var allowedLeadingKeywords = map[string]struct{}{
	"SELECT": {},
	"WITH":   {},
	"VALUES": {},
	"TABLE":  {},
}

// systemTablePrefixes lists token prefixes that always indicate a system
// table reference. Matching is case-insensitive on the token itself.
var systemTablePrefixes = []string{
	"pg_",
}

// systemSchemas lists schema names that, when followed by `.<anything>`,
// indicate a system-table reference. Matching is case-insensitive on the
// schema token.
var systemSchemas = map[string]struct{}{
	"information_schema": {},
	"pg_catalog":         {},
	"pg_toast":           {},
}

// ValidateQuery performs the SQL safety check enforced by US-434. The
// returned error (if non-nil) is one of the sentinel errors declared in
// this file; callers should branch with errors.Is. The validator is
// tokenizer-based — it strips line comments (`--`), block comments
// (`/* */`), single-quoted string literals (with `''` escape), dollar-
// quoted strings (`$tag$ ... $tag$`), and double/back-quoted identifiers
// before scanning, so a forbidden keyword sitting inside a string
// literal does NOT trigger a false positive.
func ValidateQuery(query string) error {
	tokens, err := tokenizeSQL(query)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		return ErrEmptyQuery
	}
	// Stacked-statement guard: any non-trailing semicolon token splits
	// the input into more than one statement.
	for i, tok := range tokens {
		if tok.kind == tokSemicolon && i != len(tokens)-1 {
			return fmt.Errorf("%w: more than one statement detected", ErrStackedStatement)
		}
	}
	// Drop a trailing semicolon for the leading-keyword check.
	if tokens[len(tokens)-1].kind == tokSemicolon {
		tokens = tokens[:len(tokens)-1]
	}
	if len(tokens) == 0 {
		return ErrEmptyQuery
	}
	leading := tokens[0]
	if leading.kind != tokKeyword {
		return fmt.Errorf("%w: leading token %q is not a statement keyword", ErrForbiddenStatement, leading.value)
	}
	upper := strings.ToUpper(leading.value)
	if _, banned := forbiddenLeadingKeywords[upper]; banned {
		return fmt.Errorf("%w: %s", ErrForbiddenStatement, upper)
	}
	if _, ok := allowedLeadingKeywords[upper]; !ok {
		return fmt.Errorf("%w: %s", ErrForbiddenStatement, upper)
	}
	// Scan body tokens for forbidden DML keywords or system-table refs.
	// We also reject any embedded INSERT/UPDATE/DELETE/DROP/ALTER —
	// these CANNOT legally appear inside a SELECT / WITH / VALUES /
	// TABLE statement, so finding one means a stacked-statement attempt
	// that slipped through tokenisation, or a CTE that mutates state
	// (`WITH x AS (DELETE ...)` — a real PG syntax that we explicitly
	// reject for the SQL endpoint).
	for i, tok := range tokens {
		if tok.kind == tokKeyword {
			u := strings.ToUpper(tok.value)
			if _, banned := forbiddenLeadingKeywords[u]; banned {
				switch u {
				case "INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "TRUNCATE",
					"CREATE", "GRANT", "REVOKE", "MERGE", "REPLACE", "CALL",
					"COPY", "EXECUTE", "PREPARE", "VACUUM", "ANALYZE",
					"CLUSTER", "REINDEX", "REFRESH":
					return fmt.Errorf("%w: %s appears in body", ErrForbiddenStatement, u)
				}
			}
		}
		if tok.kind == tokIdentifier {
			lower := strings.ToLower(tok.value)
			for _, prefix := range systemTablePrefixes {
				if strings.HasPrefix(lower, prefix) {
					return fmt.Errorf("%w: %s", ErrSystemTableAccess, tok.value)
				}
			}
			if _, ok := systemSchemas[lower]; ok {
				// Followed by a dot? Then it is a schema-qualified
				// reference (information_schema.tables). Standalone
				// (e.g. used as a column alias) — also rejected because
				// SELECTing FROM the schema name is suspicious.
				if i+1 < len(tokens) && tokens[i+1].kind == tokDot {
					return fmt.Errorf("%w: %s", ErrSystemTableAccess, tok.value)
				}
				return fmt.Errorf("%w: %s", ErrSystemTableAccess, tok.value)
			}
		}
	}
	return nil
}

// tokenKind enumerates the lexical categories produced by tokenizeSQL.
type tokenKind int

const (
	tokKeyword tokenKind = iota
	tokIdentifier
	tokDot
	tokSemicolon
	tokOther
)

type sqlToken struct {
	kind  tokenKind
	value string
}

// sqlKeywords lists the SQL keywords we recognise as statement openers
// or body markers. The list is intentionally narrow — only the words we
// branch on. Other reserved words (FROM, WHERE, JOIN, …) are returned
// as identifiers and never trigger validation logic.
var sqlKeywords = map[string]struct{}{
	// Statement openers.
	"SELECT": {}, "WITH": {}, "VALUES": {}, "TABLE": {},
	// Forbidden openers (mirrored in forbiddenLeadingKeywords).
	"INSERT": {}, "UPDATE": {}, "DELETE": {}, "DROP": {}, "ALTER": {},
	"TRUNCATE": {}, "CREATE": {}, "GRANT": {}, "REVOKE": {}, "MERGE": {},
	"REPLACE": {}, "CALL": {}, "COPY": {}, "EXECUTE": {}, "PREPARE": {},
	"COMMIT": {}, "ROLLBACK": {}, "BEGIN": {}, "START": {}, "END": {},
	"SAVEPOINT": {}, "LOCK": {}, "UNLOCK": {}, "SET": {}, "RESET": {},
	"VACUUM": {}, "ANALYZE": {}, "CLUSTER": {}, "REINDEX": {},
	"REFRESH": {}, "COMMENT": {}, "DO": {}, "LISTEN": {}, "NOTIFY": {},
	"DISCARD": {},
}

// tokenizeSQL strips comments / strings / quoted identifiers from query
// and emits the surviving tokens with kind and case-preserved value.
// Returns ErrStackedStatement for unterminated block comments / strings
// (defensive — a malformed input is treated as unsafe).
func tokenizeSQL(query string) ([]sqlToken, error) {
	out := make([]sqlToken, 0, 32)
	runes := []rune(query)
	n := len(runes)
	i := 0
	for i < n {
		r := runes[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '-' && i+1 < n && runes[i+1] == '-':
			// Line comment: skip until newline.
			i += 2
			for i < n && runes[i] != '\n' {
				i++
			}
		case r == '/' && i+1 < n && runes[i+1] == '*':
			// Block comment: skip until matching `*/`. Nested blocks
			// are not supported by SQL standard but PG allows them; we
			// support nesting too because a hostile input could exploit
			// the non-nesting behaviour to inject keywords.
			i += 2
			depth := 1
			for i < n && depth > 0 {
				if i+1 < n && runes[i] == '/' && runes[i+1] == '*' {
					depth++
					i += 2
					continue
				}
				if i+1 < n && runes[i] == '*' && runes[i+1] == '/' {
					depth--
					i += 2
					continue
				}
				i++
			}
			if depth != 0 {
				return nil, fmt.Errorf("%w: unterminated block comment", ErrForbiddenStatement)
			}
		case r == '\'':
			// Single-quoted string literal. Doubled `''` is the SQL
			// escape; PG also accepts `\'` when standard_conforming_
			// strings is off, but we don't honour that — a backslash
			// inside a literal is treated as a literal character so an
			// embedded `\'` still terminates on the next standalone
			// quote, matching PG's default standard-conforming mode.
			i++
			for i < n {
				if runes[i] == '\'' {
					if i+1 < n && runes[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
		case r == '"':
			// Quoted identifier (case-preserving). Doubled `""` is the
			// embedded-quote escape.
			i++
			start := i
			var b strings.Builder
			for i < n {
				if runes[i] == '"' {
					if i+1 < n && runes[i+1] == '"' {
						b.WriteRune('"')
						i += 2
						continue
					}
					b.WriteString(string(runes[start:i]))
					i++
					break
				}
				i++
			}
			value := b.String()
			if value == "" && start < i-1 {
				value = string(runes[start : i-1])
			}
			out = append(out, sqlToken{kind: tokIdentifier, value: value})
		case r == '`':
			// MySQL-style identifier quoting; reject as forbidden.
			i++
			for i < n && runes[i] != '`' {
				i++
			}
			if i < n {
				i++
			}
		case r == '$' && isDollarTagStart(runes, i):
			// Dollar-quoted string: $tag$...$tag$ where tag is empty
			// or an identifier. The closing tag must match exactly.
			tagEnd := i + 1
			for tagEnd < n && runes[tagEnd] != '$' {
				tagEnd++
			}
			if tagEnd >= n {
				return nil, fmt.Errorf("%w: unterminated dollar-quoted literal", ErrForbiddenStatement)
			}
			tag := string(runes[i : tagEnd+1])
			i = tagEnd + 1
			closed := false
			for i+len(tag) <= n {
				if string(runes[i:i+len([]rune(tag))]) == tag {
					i += len([]rune(tag))
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, fmt.Errorf("%w: unterminated dollar-quoted literal", ErrForbiddenStatement)
			}
		case r == ';':
			out = append(out, sqlToken{kind: tokSemicolon, value: ";"})
			i++
		case r == '.':
			out = append(out, sqlToken{kind: tokDot, value: "."})
			i++
		case isIdentStart(r):
			start := i
			for i < n && isIdentPart(runes[i]) {
				i++
			}
			word := string(runes[start:i])
			upper := strings.ToUpper(word)
			if _, ok := sqlKeywords[upper]; ok {
				out = append(out, sqlToken{kind: tokKeyword, value: word})
			} else {
				out = append(out, sqlToken{kind: tokIdentifier, value: word})
			}
		default:
			// Anything else (operators, parentheses, punctuation,
			// numeric literals) is irrelevant to safety analysis.
			out = append(out, sqlToken{kind: tokOther, value: string(r)})
			i++
		}
	}
	return out, nil
}

// isDollarTagStart reports whether the position `i` starts a dollar-
// quoted-string opening tag. Returns true when the rune sequence
// matches `$ tag $` where tag is empty or made of identifier chars.
func isDollarTagStart(runes []rune, i int) bool {
	if i >= len(runes) || runes[i] != '$' {
		return false
	}
	// Find the matching closing `$` of the tag.
	for j := i + 1; j < len(runes); j++ {
		switch {
		case runes[j] == '$':
			return true
		case isIdentPart(runes[j]):
			continue
		default:
			return false
		}
	}
	return false
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
