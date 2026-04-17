package sdkgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// MetadataFilename is the relative path of the metadata file emitted with
// every generated SDK. It is the same across all languages so tooling can
// locate it without knowing the language.
const MetadataFilename = ".weave-sdk.json"

// ChangelogFilename is the relative path of the generated CHANGELOG.md.
const ChangelogFilename = "CHANGELOG.md"

// SDKMetadata is the JSON document emitted with each generated SDK so that
// downstream tooling (CLI --diff, CI) can tell what ontology version the SDK
// was generated from.
type SDKMetadata struct {
	OntologyAPIName string          `json:"ontologyApiName"`
	OntologyRID     string          `json:"ontologyRid"`
	OntologyVersion int             `json:"ontologyVersion"`
	GeneratedAt     time.Time       `json:"generatedAt"`
	ServerURL       string          `json:"serverUrl"`
	Language        string          `json:"language,omitempty"`
	ObjectTypes     []string        `json:"objectTypes"`
	LinkTypes       []string        `json:"linkTypes"`
	ActionTypes     []string        `json:"actionTypes"`
	Schema          *OntologySchema `json:"schema,omitempty"`
}

// BuildMetadata populates an SDKMetadata document from the given schema plus
// runtime context (server URL, generated-at timestamp, target language).
// APIName lists are sorted for stable serialization.
func BuildMetadata(schema OntologySchema, serverURL string, language string, generatedAt time.Time) SDKMetadata {
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	generatedAt = generatedAt.UTC()

	objects := make([]string, 0, len(schema.ObjectTypes))
	for _, ot := range schema.ObjectTypes {
		objects = append(objects, ot.APIName)
	}
	links := make([]string, 0, len(schema.LinkTypes))
	for _, lt := range schema.LinkTypes {
		links = append(links, lt.APIName)
	}
	actions := make([]string, 0, len(schema.ActionTypes))
	for _, at := range schema.ActionTypes {
		actions = append(actions, at.APIName)
	}
	sort.Strings(objects)
	sort.Strings(links)
	sort.Strings(actions)

	snapshot := schema
	snapshot.ServerURL = ""
	snapshot.GeneratedAt = time.Time{}
	snapshot.Previous = nil

	return SDKMetadata{
		OntologyAPIName: schema.Ontology.APIName,
		OntologyRID:     schema.Ontology.RID,
		OntologyVersion: schema.Ontology.Version,
		GeneratedAt:     generatedAt,
		ServerURL:       serverURL,
		Language:        language,
		ObjectTypes:     objects,
		LinkTypes:       links,
		ActionTypes:     actions,
		Schema:          &snapshot,
	}
}

// MarshalMetadata serializes metadata as indented JSON.
func MarshalMetadata(m SDKMetadata) []byte {
	out, _ := json.MarshalIndent(m, "", "  ")
	return append(out, '\n')
}

// ObjectTypeChange describes a property-level change inside a modified
// ObjectType in a SchemaDiff.
type ObjectTypeChange struct {
	APIName            string
	AddedProperties    []string
	RemovedProperties  []string
	ModifiedProperties []string
}

// SchemaDiff is the result of comparing two ontology schemas.
type SchemaDiff struct {
	OldVersion       int
	NewVersion       int
	OntologyAPIName  string
	AddedObjects     []ObjectTypeSchema
	RemovedObjects   []ObjectTypeSchema
	ModifiedObjects  []ObjectTypeChange
	AddedLinks       []LinkTypeSchema
	RemovedLinks     []LinkTypeSchema
	ModifiedLinks    []LinkTypeSchema
	AddedActions     []ActionTypeSchema
	RemovedActions   []ActionTypeSchema
	ModifiedActions  []ActionTypeSchema
}

// HasChanges reports whether the diff describes any schema change.
func (d SchemaDiff) HasChanges() bool {
	return len(d.AddedObjects) > 0 ||
		len(d.RemovedObjects) > 0 ||
		len(d.ModifiedObjects) > 0 ||
		len(d.AddedLinks) > 0 ||
		len(d.RemovedLinks) > 0 ||
		len(d.ModifiedLinks) > 0 ||
		len(d.AddedActions) > 0 ||
		len(d.RemovedActions) > 0 ||
		len(d.ModifiedActions) > 0
}

// DiffSchemas compares two OntologySchema snapshots and reports the diff.
// A nil oldSchema treats every entity in newSchema as added.
func DiffSchemas(oldSchema *OntologySchema, newSchema OntologySchema) SchemaDiff {
	diff := SchemaDiff{
		NewVersion:      newSchema.Ontology.Version,
		OntologyAPIName: newSchema.Ontology.APIName,
	}
	if oldSchema != nil {
		diff.OldVersion = oldSchema.Ontology.Version
	}

	// --- ObjectTypes ---
	oldObjects := map[string]ObjectTypeSchema{}
	if oldSchema != nil {
		for _, ot := range oldSchema.ObjectTypes {
			oldObjects[ot.APIName] = ot
		}
	}
	newObjects := map[string]ObjectTypeSchema{}
	for _, ot := range newSchema.ObjectTypes {
		newObjects[ot.APIName] = ot
	}

	for _, name := range sortedKeys(newObjects) {
		n := newObjects[name]
		o, existed := oldObjects[name]
		if !existed {
			diff.AddedObjects = append(diff.AddedObjects, n)
			continue
		}
		change := diffProperties(name, o.Properties, n.Properties)
		if len(change.AddedProperties) > 0 || len(change.RemovedProperties) > 0 || len(change.ModifiedProperties) > 0 {
			diff.ModifiedObjects = append(diff.ModifiedObjects, change)
		}
	}
	for _, name := range sortedKeys(oldObjects) {
		if _, stillPresent := newObjects[name]; !stillPresent {
			diff.RemovedObjects = append(diff.RemovedObjects, oldObjects[name])
		}
	}

	// --- LinkTypes (structural identity: APIName+source+target+cardinality) ---
	oldLinks := indexLinkTypes(nilOr(oldSchema).LinkTypes)
	newLinks := indexLinkTypes(newSchema.LinkTypes)
	for _, key := range sortedKeys(newLinks) {
		if prev, ok := oldLinks[key]; ok {
			if prev != newLinks[key] {
				diff.ModifiedLinks = append(diff.ModifiedLinks, newLinks[key])
			}
			continue
		}
		// Not in old — maybe APIName existed but signature changed? Keep it simple: report added.
		diff.AddedLinks = append(diff.AddedLinks, newLinks[key])
	}
	for _, key := range sortedKeys(oldLinks) {
		if _, stillPresent := newLinks[key]; !stillPresent {
			diff.RemovedLinks = append(diff.RemovedLinks, oldLinks[key])
		}
	}

	// --- ActionTypes ---
	oldActions := indexActionTypes(nilOr(oldSchema).ActionTypes)
	newActions := indexActionTypes(newSchema.ActionTypes)
	for _, name := range sortedKeys(newActions) {
		n := newActions[name]
		o, existed := oldActions[name]
		if !existed {
			diff.AddedActions = append(diff.AddedActions, n)
			continue
		}
		if !actionParamsEqual(o.Parameters, n.Parameters) || o.DisplayName != n.DisplayName {
			diff.ModifiedActions = append(diff.ModifiedActions, n)
		}
	}
	for _, name := range sortedKeys(oldActions) {
		if _, stillPresent := newActions[name]; !stillPresent {
			diff.RemovedActions = append(diff.RemovedActions, oldActions[name])
		}
	}

	return diff
}

func diffProperties(objectName string, oldProps, newProps []PropertySchema) ObjectTypeChange {
	change := ObjectTypeChange{APIName: objectName}
	oldByName := map[string]PropertySchema{}
	for _, p := range oldProps {
		oldByName[p.APIName] = p
	}
	newByName := map[string]PropertySchema{}
	for _, p := range newProps {
		newByName[p.APIName] = p
	}
	for _, name := range sortedKeys(newByName) {
		n := newByName[name]
		o, existed := oldByName[name]
		if !existed {
			change.AddedProperties = append(change.AddedProperties, name)
			continue
		}
		if o.BaseType != n.BaseType || o.IsArray != n.IsArray {
			change.ModifiedProperties = append(change.ModifiedProperties, name)
		}
	}
	for _, name := range sortedKeys(oldByName) {
		if _, stillPresent := newByName[name]; !stillPresent {
			change.RemovedProperties = append(change.RemovedProperties, name)
		}
	}
	return change
}

func indexLinkTypes(links []LinkTypeSchema) map[string]LinkTypeSchema {
	out := make(map[string]LinkTypeSchema, len(links))
	for _, lt := range links {
		out[lt.APIName] = lt
	}
	return out
}

func indexActionTypes(actions []ActionTypeSchema) map[string]ActionTypeSchema {
	out := make(map[string]ActionTypeSchema, len(actions))
	for _, at := range actions {
		out[at.APIName] = at
	}
	return out
}

func actionParamsEqual(a, b []ActionParamSchema) bool {
	if len(a) != len(b) {
		return false
	}
	aByID := make(map[string]ActionParamSchema, len(a))
	for _, p := range a {
		aByID[p.ID] = p
	}
	for _, p := range b {
		other, ok := aByID[p.ID]
		if !ok || other != p {
			return false
		}
	}
	return true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// nilOr returns a zero OntologySchema if s is nil; otherwise *s.
func nilOr(s *OntologySchema) OntologySchema {
	if s == nil {
		return OntologySchema{}
	}
	return *s
}

// FormatChangelog produces a Markdown changelog describing the diff.
// When the diff has no prior version, the header reads "Initial release".
func FormatChangelog(diff SchemaDiff, generatedAt time.Time) []byte {
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	generatedAt = generatedAt.UTC()

	var buf bytes.Buffer
	buf.WriteString("# Changelog\n\n")

	header := fmt.Sprintf("## Version %d — %s", diff.NewVersion, generatedAt.Format("2006-01-02"))
	if diff.OldVersion == 0 && !diff.HasChanges() {
		buf.WriteString(header + " (initial release)\n\n")
		buf.WriteString("_No entities in ontology._\n")
		return buf.Bytes()
	}
	if diff.OldVersion == 0 {
		buf.WriteString(header + " (initial release)\n\n")
	} else {
		fmt.Fprintf(&buf, "%s (from version %d)\n\n", header, diff.OldVersion)
	}

	writeSection := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&buf, "### %s\n\n", title)
		for _, item := range items {
			fmt.Fprintf(&buf, "- %s\n", item)
		}
		buf.WriteString("\n")
	}

	addedObjs := make([]string, 0, len(diff.AddedObjects))
	for _, ot := range diff.AddedObjects {
		addedObjs = append(addedObjs, fmt.Sprintf("ObjectType **%s** (%d properties)", ot.APIName, len(ot.Properties)))
	}
	writeSection("Added ObjectTypes", addedObjs)

	removedObjs := make([]string, 0, len(diff.RemovedObjects))
	for _, ot := range diff.RemovedObjects {
		removedObjs = append(removedObjs, fmt.Sprintf("ObjectType **%s**", ot.APIName))
	}
	writeSection("Removed ObjectTypes", removedObjs)

	if len(diff.ModifiedObjects) > 0 {
		buf.WriteString("### Modified ObjectTypes\n\n")
		for _, m := range diff.ModifiedObjects {
			fmt.Fprintf(&buf, "- **%s**\n", m.APIName)
			for _, p := range m.AddedProperties {
				fmt.Fprintf(&buf, "  - added property `%s`\n", p)
			}
			for _, p := range m.RemovedProperties {
				fmt.Fprintf(&buf, "  - removed property `%s`\n", p)
			}
			for _, p := range m.ModifiedProperties {
				fmt.Fprintf(&buf, "  - modified property `%s`\n", p)
			}
		}
		buf.WriteString("\n")
	}

	addedLinks := make([]string, 0, len(diff.AddedLinks))
	for _, lt := range diff.AddedLinks {
		addedLinks = append(addedLinks, fmt.Sprintf("LinkType **%s** (%s → %s)", lt.APIName, lt.SourceObjectType, lt.TargetObjectType))
	}
	writeSection("Added LinkTypes", addedLinks)

	removedLinks := make([]string, 0, len(diff.RemovedLinks))
	for _, lt := range diff.RemovedLinks {
		removedLinks = append(removedLinks, fmt.Sprintf("LinkType **%s**", lt.APIName))
	}
	writeSection("Removed LinkTypes", removedLinks)

	modifiedLinks := make([]string, 0, len(diff.ModifiedLinks))
	for _, lt := range diff.ModifiedLinks {
		modifiedLinks = append(modifiedLinks, fmt.Sprintf("LinkType **%s**", lt.APIName))
	}
	writeSection("Modified LinkTypes", modifiedLinks)

	addedActions := make([]string, 0, len(diff.AddedActions))
	for _, at := range diff.AddedActions {
		addedActions = append(addedActions, fmt.Sprintf("ActionType **%s**", at.APIName))
	}
	writeSection("Added ActionTypes", addedActions)

	removedActions := make([]string, 0, len(diff.RemovedActions))
	for _, at := range diff.RemovedActions {
		removedActions = append(removedActions, fmt.Sprintf("ActionType **%s**", at.APIName))
	}
	writeSection("Removed ActionTypes", removedActions)

	modifiedActions := make([]string, 0, len(diff.ModifiedActions))
	for _, at := range diff.ModifiedActions {
		modifiedActions = append(modifiedActions, fmt.Sprintf("ActionType **%s**", at.APIName))
	}
	writeSection("Modified ActionTypes", modifiedActions)

	return buf.Bytes()
}
