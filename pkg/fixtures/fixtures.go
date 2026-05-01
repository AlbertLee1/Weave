// Package fixtures generates synthetic test data for an Ontology ObjectType.
//
// Given a list of [PropertyDef] (a flat, type-aware view of the object's
// properties) and a desired row count, [Generate] returns deterministic rows
// that respect each property's BaseType, isArray / isNullable flags, and the
// constraint set carried in TypeConfig (regex, minLength / maxLength, min /
// max, enum). Primary-key properties always receive unique non-null values.
//
// The generator is seedable so a fixed seed always produces the same output
// — this matters for "weave fixtures generate" piping into a regression
// suite where the data has to round-trip across runs.
package fixtures

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"regexp"
	"regexp/syntax"
	"sort"
	"strings"
	"time"
)

// PropertyDef is a normalised view of a property the generator can synthesise
// values for. It is intentionally decoupled from oms.Property: the CLI loads
// metadata over the wire and converts to this shape via [PropertyDefsFromWire].
type PropertyDef struct {
	APIName    string
	BaseType   string
	IsArray    bool
	IsNullable bool
	// IsPrimary forces the generator to (a) skip the null branch even when
	// IsNullable is true, and (b) ensure cross-row uniqueness on the chosen
	// value within a single Generate call.
	IsPrimary bool

	Regex     string
	MinLength *int
	MaxLength *int
	Min       *float64
	Max       *float64
	Enum      []any
}

// Options controls fixture generation behaviour.
type Options struct {
	// Seed deterministically seeds the underlying RNG. A zero seed picks a
	// fresh time-based seed each call.
	Seed int64
	// NullRatio is the probability (0.0–1.0) that a non-primary nullable
	// property emits null on a given row. Defaults to 0.0 — no nulls.
	NullRatio float64
}

// Generate returns count rows shaped per props. Each row is a flat
// map[string]any keyed by APIName.
func Generate(props []PropertyDef, count int, opts Options) ([]map[string]any, error) {
	if count < 0 {
		return nil, fmt.Errorf("count must be non-negative, got %d", count)
	}
	if opts.NullRatio < 0 || opts.NullRatio > 1 {
		return nil, fmt.Errorf("nullRatio must be in [0,1], got %v", opts.NullRatio)
	}

	seed := opts.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))

	g := &generator{
		rng:        rng,
		nullRatio:  opts.NullRatio,
		uniquePool: map[string]map[string]struct{}{},
	}

	rows := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		row := make(map[string]any, len(props))
		for _, p := range props {
			v, err := g.value(p, i)
			if err != nil {
				return nil, fmt.Errorf("row %d, property %q: %w", i, p.APIName, err)
			}
			row[p.APIName] = v
		}
		rows = append(rows, row)
	}
	return rows, nil
}

type generator struct {
	rng        *rand.Rand
	nullRatio  float64
	uniquePool map[string]map[string]struct{}
}

func (g *generator) value(p PropertyDef, rowIdx int) (any, error) {
	if !p.IsPrimary && p.IsNullable && g.nullRatio > 0 && g.rng.Float64() < g.nullRatio {
		return nil, nil
	}
	if p.IsArray {
		n := g.rng.Intn(3) + 1
		out := make([]any, 0, n)
		scalar := p
		scalar.IsArray = false
		scalar.IsPrimary = false
		for i := 0; i < n; i++ {
			v, err := g.scalar(scalar, rowIdx)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}
	return g.scalar(p, rowIdx)
}

func (g *generator) scalar(p PropertyDef, rowIdx int) (any, error) {
	if len(p.Enum) > 0 {
		if p.IsPrimary {
			return nil, fmt.Errorf("enum cannot satisfy primary-key uniqueness for %q", p.APIName)
		}
		return p.Enum[g.rng.Intn(len(p.Enum))], nil
	}

	switch normaliseBaseType(p.BaseType) {
	case "string":
		return g.stringValue(p, rowIdx)
	case "integer", "short", "byte":
		return g.intValue(p, rowIdx, 32), nil
	case "long":
		return g.intValue(p, rowIdx, 64), nil
	case "float", "double", "decimal":
		return g.floatValue(p), nil
	case "boolean":
		return g.rng.Intn(2) == 0, nil
	case "date":
		return g.dateValue(), nil
	case "timestamp":
		return g.timestampValue(), nil
	default:
		return g.stringValue(p, rowIdx)
	}
}

func (g *generator) stringValue(p PropertyDef, rowIdx int) (string, error) {
	if p.Regex != "" {
		s, err := g.regexString(p)
		if err != nil {
			return "", err
		}
		if p.IsPrimary {
			return g.ensureUnique(p.APIName, s, func() (string, error) { return g.regexString(p) })
		}
		return s, nil
	}

	minLen := derefInt(p.MinLength, 3)
	maxLen := derefInt(p.MaxLength, 16)
	if maxLen < minLen {
		maxLen = minLen
	}
	if p.IsPrimary {
		return g.uniqueAlnum(p.APIName, rowIdx, minLen, maxLen), nil
	}
	return g.alnum(minLen+g.rng.Intn(maxLen-minLen+1)), nil
}

func (g *generator) regexString(p PropertyDef) (string, error) {
	re, err := regexp.Compile(p.Regex)
	if err != nil {
		return "", fmt.Errorf("invalid regex %q: %w", p.Regex, err)
	}
	parsed, err := syntax.Parse(p.Regex, syntax.Perl)
	if err != nil {
		return "", fmt.Errorf("invalid regex %q: %w", p.Regex, err)
	}
	minLen := derefInt(p.MinLength, 0)
	maxLen := derefInt(p.MaxLength, 64)
	if maxLen < minLen {
		maxLen = minLen
	}
	for attempt := 0; attempt < 200; attempt++ {
		s := g.synthFromRegex(parsed)
		if minLen > 0 && len(s) < minLen {
			continue
		}
		if maxLen > 0 && len(s) > maxLen {
			continue
		}
		if re.MatchString(s) {
			return s, nil
		}
	}
	return "", fmt.Errorf("could not satisfy regex %q within length [%d,%d] after 200 attempts", p.Regex, minLen, maxLen)
}

// synthFromRegex produces a string from a regexp syntax tree. It covers the
// operators that arise from typical ValueType regex constraints — character
// classes, repeats with bounds, concatenation, alternation, simple literals
// — and returns a best-effort empty string for ops the OSS regex world rarely
// uses (back-references, lookaheads). The caller validates with the compiled
// regex, so anything not actually accepted will be retried.
func (g *generator) synthFromRegex(re *syntax.Regexp) string {
	switch re.Op {
	case syntax.OpEmptyMatch, syntax.OpBeginLine, syntax.OpEndLine,
		syntax.OpBeginText, syntax.OpEndText,
		syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return ""
	case syntax.OpLiteral:
		return string(re.Rune)
	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return string(rune('a' + g.rng.Intn(26)))
	case syntax.OpCharClass:
		return g.pickFromClass(re.Rune)
	case syntax.OpCapture:
		return g.synthFromRegex(re.Sub[0])
	case syntax.OpConcat:
		var b strings.Builder
		for _, s := range re.Sub {
			b.WriteString(g.synthFromRegex(s))
		}
		return b.String()
	case syntax.OpAlternate:
		if len(re.Sub) == 0 {
			return ""
		}
		return g.synthFromRegex(re.Sub[g.rng.Intn(len(re.Sub))])
	case syntax.OpStar:
		return g.repeat(re.Sub[0], 0, 4)
	case syntax.OpPlus:
		return g.repeat(re.Sub[0], 1, 4)
	case syntax.OpQuest:
		if g.rng.Intn(2) == 0 {
			return ""
		}
		return g.synthFromRegex(re.Sub[0])
	case syntax.OpRepeat:
		min := re.Min
		max := re.Max
		if max < 0 {
			max = min + 4
		}
		return g.repeat(re.Sub[0], min, max)
	default:
		return ""
	}
}

func (g *generator) repeat(sub *syntax.Regexp, min, max int) string {
	if max < min {
		max = min
	}
	span := max - min + 1
	if span <= 0 {
		span = 1
	}
	n := min + g.rng.Intn(span)
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(g.synthFromRegex(sub))
	}
	return b.String()
}

// pickFromClass picks a rune from a syntax char-class range slice. The slice
// is laid out as pairs [lo, hi] in ascending order. We choose a pair weighted
// by its size so all admissible runes are equally likely.
func (g *generator) pickFromClass(ranges []rune) string {
	if len(ranges) < 2 {
		return ""
	}
	type span struct {
		lo, hi rune
		width  int
	}
	spans := make([]span, 0, len(ranges)/2)
	total := 0
	for i := 0; i < len(ranges); i += 2 {
		lo, hi := ranges[i], ranges[i+1]
		// Skip non-printable ranges so synthesised strings stay safe to
		// embed in JSON / SQL / shell pipes.
		if hi < 0x20 || lo > 0x7E {
			continue
		}
		if lo < 0x20 {
			lo = 0x20
		}
		if hi > 0x7E {
			hi = 0x7E
		}
		w := int(hi-lo) + 1
		spans = append(spans, span{lo, hi, w})
		total += w
	}
	if total <= 0 {
		// Fallback to the first range as-is — used by classes like \s, \n.
		lo := ranges[0]
		return string(lo)
	}
	pick := g.rng.Intn(total)
	for _, s := range spans {
		if pick < s.width {
			return string(s.lo + rune(pick))
		}
		pick -= s.width
	}
	return string(spans[len(spans)-1].lo)
}

func (g *generator) intValue(p PropertyDef, rowIdx int, bitsize int) any {
	min, max := intRange(p, bitsize)
	if p.IsPrimary {
		span := max - min
		if span <= 0 {
			span = 1
		}
		v := min + int64(rowIdx)
		if v > max {
			v = min + (int64(rowIdx) % span)
		}
		_ = g.rememberUnique(p.APIName, fmt.Sprint(v))
		if bitsize == 32 {
			return int(v)
		}
		return v
	}
	span := max - min + 1
	if span <= 0 {
		span = 1
	}
	v := min + g.rng.Int63n(span)
	if bitsize == 32 {
		return int(v)
	}
	return v
}

func (g *generator) floatValue(p PropertyDef) float64 {
	min := -1e6
	max := 1e6
	if p.Min != nil {
		min = *p.Min
	}
	if p.Max != nil {
		max = *p.Max
	}
	if max < min {
		max = min
	}
	return min + g.rng.Float64()*(max-min)
}

func (g *generator) dateValue() string {
	base := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	d := base.AddDate(0, 0, g.rng.Intn(365*30))
	return d.Format("2006-01-02")
}

func (g *generator) timestampValue() string {
	base := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	t := base.Add(time.Duration(g.rng.Int63n(int64(time.Hour) * 24 * 365 * 30)))
	return t.UTC().Format(time.RFC3339)
}

const alnumAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func (g *generator) alnum(n int) string {
	if n <= 0 {
		n = 1
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = alnumAlphabet[g.rng.Intn(len(alnumAlphabet))]
	}
	return string(b)
}

// uniqueAlnum returns an alphanumeric string of length within [minLen, maxLen]
// that has not yet been emitted under apiName. Falls back to suffixing a hex
// rowIdx if the random draw collides — primary keys must always succeed.
func (g *generator) uniqueAlnum(apiName string, rowIdx, minLen, maxLen int) string {
	for attempt := 0; attempt < 50; attempt++ {
		l := minLen + g.rng.Intn(maxLen-minLen+1)
		s := g.alnum(l)
		if g.rememberUnique(apiName, s) {
			return s
		}
	}
	suffix := fmt.Sprintf("%x", rowIdx)
	prefix := g.alnum(intMax(minLen-len(suffix), 1))
	candidate := prefix + suffix
	if len(candidate) > maxLen {
		candidate = candidate[len(candidate)-maxLen:]
	}
	g.rememberUnique(apiName, candidate)
	return candidate
}

func (g *generator) ensureUnique(apiName, candidate string, retry func() (string, error)) (string, error) {
	if g.rememberUnique(apiName, candidate) {
		return candidate, nil
	}
	for attempt := 0; attempt < 50; attempt++ {
		next, err := retry()
		if err != nil {
			return "", err
		}
		if g.rememberUnique(apiName, next) {
			return next, nil
		}
	}
	return "", fmt.Errorf("could not produce a unique value for %q after 50 attempts", apiName)
}

func (g *generator) rememberUnique(apiName, value string) bool {
	pool, ok := g.uniquePool[apiName]
	if !ok {
		pool = map[string]struct{}{}
		g.uniquePool[apiName] = pool
	}
	if _, exists := pool[value]; exists {
		return false
	}
	pool[value] = struct{}{}
	return true
}

func intRange(p PropertyDef, bitsize int) (int64, int64) {
	var min, max int64
	switch bitsize {
	case 32:
		min, max = -1_000_000, 1_000_000
	default:
		min, max = -1_000_000_000, 1_000_000_000
	}
	if p.Min != nil {
		min = int64(*p.Min)
	}
	if p.Max != nil {
		max = int64(*p.Max)
	}
	if max < min {
		max = min
	}
	return min, max
}

func derefInt(p *int, fallback int) int {
	if p == nil {
		return fallback
	}
	return *p
}

func intMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func normaliseBaseType(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// PropertyDefsFromWire flattens the V2 ObjectType.properties wire shape into
// PropertyDefs. The map is the value at `properties` in either the regular
// ObjectType payload or the fullMetadata payload — both shapes carry `dataType`
// per entry. primaryKeys marks the result entries with matching APIName as
// IsPrimary.
//
// Only baseType / isArray / regex / minLength / maxLength / min / max / enum
// fields are extracted; every other typeConfig key is silently ignored, so
// callers don't need to keep this in sync with the full BaseType matrix.
func PropertyDefsFromWire(properties map[string]any, primaryKeys []string) ([]PropertyDef, error) {
	pkSet := map[string]struct{}{}
	for _, k := range primaryKeys {
		pkSet[k] = struct{}{}
	}

	names := make([]string, 0, len(properties))
	for k := range properties {
		names = append(names, k)
	}
	sort.Strings(names)

	out := make([]PropertyDef, 0, len(properties))
	for _, name := range names {
		raw := properties[name]
		entry, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("property %q: expected object, got %T", name, raw)
		}
		dt, _ := entry["dataType"].(map[string]any)
		def := PropertyDef{APIName: name}
		if _, isPK := pkSet[name]; isPK {
			def.IsPrimary = true
		}

		def.BaseType = stringField(dt, "type")
		if def.BaseType == "array" {
			def.IsArray = true
			if sub, ok := dt["subType"].(map[string]any); ok {
				def.BaseType = stringField(sub, "type")
				mergeConstraints(&def, sub)
			}
		}
		mergeConstraints(&def, dt)

		// Top-level isNullable / nullable hints (rarely emitted via wire today
		// but harmless if present in user-supplied JSON).
		if b, ok := entry["isNullable"].(bool); ok {
			def.IsNullable = b
		} else if b, ok := entry["nullable"].(bool); ok {
			def.IsNullable = b
		}
		out = append(out, def)
	}
	return out, nil
}

func mergeConstraints(def *PropertyDef, dt map[string]any) {
	if dt == nil {
		return
	}
	if s, ok := dt["regex"].(string); ok && s != "" {
		def.Regex = s
	}
	if i, ok := numField(dt["minLength"]); ok {
		v := int(i)
		def.MinLength = &v
	}
	if i, ok := numField(dt["maxLength"]); ok {
		v := int(i)
		def.MaxLength = &v
	}
	if f, ok := numField(dt["min"]); ok {
		v := f
		def.Min = &v
	}
	if f, ok := numField(dt["max"]); ok {
		v := f
		def.Max = &v
	}
	if arr, ok := dt["enum"].([]any); ok && len(arr) > 0 {
		def.Enum = append(def.Enum, arr...)
	}
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func numField(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	}
	return 0, false
}

// HashSeed turns a textual identifier into a deterministic int64 seed.
// Useful when callers want "same input string → same fixtures" without
// having to track an explicit numeric seed.
func HashSeed(label string) int64 {
	if label == "" {
		return 0
	}
	sum := sha256.Sum256([]byte(label))
	return int64(binary.BigEndian.Uint64(sum[:8]))
}
