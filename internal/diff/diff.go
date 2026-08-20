// Package diff computes a field-level difference between two schema versions,
// describing how the contract evolved: which fields were added, which were
// removed, and which had their type, required-flag, enum set or default value
// changed. The result is sorted deterministically so the same pair of schemas
// always produces the same diff.
package diff

import (
	"encoding/json"
	"sort"

	"task102-schemaregistry/internal/schema"
)

// ChangeKind categorizes a single field-level change.
type ChangeKind string

const (
	KindAdded           ChangeKind = "added"
	KindRemoved         ChangeKind = "removed"
	KindTypeChanged     ChangeKind = "type_changed"
	KindRequiredChanged ChangeKind = "required_changed"
	KindEnumChanged     ChangeKind = "enum_changed"
	KindDefaultChanged  ChangeKind = "default_changed"
)

// Change describes a single field change between two schema versions.
type Change struct {
	Field string          `json:"field"`
	Kind  ChangeKind      `json:"kind"`
	From  json.RawMessage `json:"from,omitempty"`
	To    json.RawMessage `json:"to,omitempty"`
}

// Result is the structured difference between two schemas.
type Result struct {
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	Changed []Change `json:"changed,omitempty"`
}

// HasChanges reports whether the two schemas differ in any way.
func (r *Result) HasChanges() bool {
	return len(r.Added) > 0 || len(r.Removed) > 0 || len(r.Changed) > 0
}

// Compute returns the field-level difference between old and newer. Added and
// removed lists are sorted by field name; changed entries are sorted by field
// name and, for a given field, by kind.
func Compute(old, newer *schema.Schema) *Result {
	oldMap := old.ByName()
	newMap := newer.ByName()

	var added, removed []string
	for name := range newMap {
		if _, ok := oldMap[name]; !ok {
			added = append(added, name)
		}
	}
	for name := range oldMap {
		if _, ok := newMap[name]; !ok {
			removed = append(removed, name)
		}
	}

	var changed []Change
	for name, o := range oldMap {
		n, ok := newMap[name]
		if !ok {
			continue
		}
		changed = appendFieldChanges(changed, name, o, n)
	}

	sort.Strings(added)
	sort.Strings(removed)
	sort.Slice(changed, func(i, j int) bool {
		if changed[i].Field != changed[j].Field {
			return changed[i].Field < changed[j].Field
		}
		return changed[i].Kind < changed[j].Kind
	})

	return &Result{Added: added, Removed: removed, Changed: changed}
}

// appendFieldChanges inspects a field present in both schemas and appends any
// changes to its type, required-flag, enum set or default value.
func appendFieldChanges(changed []Change, name string, o, n *schema.Field) []Change {
	if o.Type != n.Type {
		changed = append(changed, Change{
			Field: name, Kind: KindTypeChanged,
			From: jsonRaw(string(o.Type)), To: jsonRaw(string(n.Type)),
		})
	}
	if o.Required != n.Required {
		changed = append(changed, Change{
			Field: name, Kind: KindRequiredChanged,
			From: jsonRaw(boolStr(o.Required)), To: jsonRaw(boolStr(n.Required)),
		})
	}
	if !enumEqual(o.Enum, n.Enum) {
		changed = append(changed, Change{
			Field: name, Kind: KindEnumChanged,
			From: enumJSON(o.Enum), To: enumJSON(n.Enum),
		})
	}
	if !defaultEqual(o, n) {
		changed = append(changed, Change{
			Field: name, Kind: KindDefaultChanged,
			From: o.Default, To: n.Default,
		})
	}
	return changed
}

// enumEqual reports whether two enum slices contain the same values regardless
// of order or repetition.
func enumEqual(a, b []string) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	return setEqual(a, b)
}

func setEqual(a, b []string) bool {
	sa := toSet(a)
	sb := toSet(b)
	if len(sa) != len(sb) {
		return false
	}
	for k := range sa {
		if !sb[k] {
			return false
		}
	}
	return true
}

func toSet(s []string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, v := range s {
		m[v] = true
	}
	return m
}

// defaultEqual reports whether two fields have an equal default (both absent,
// or both present with byte-identical canonical form).
func defaultEqual(o, n *schema.Field) bool {
	if o.HasDefault() != n.HasDefault() {
		return false
	}
	if !o.HasDefault() {
		return true
	}
	return byteEqual(o.Default, n.Default)
}

func byteEqual(a, b json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// jsonRaw returns a json.RawMessage for a plain string value.
func jsonRaw(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return json.RawMessage(b)
}

// boolStr renders a bool as the JSON literal "true" or "false".
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// enumJSON marshals an enum slice to a sorted JSON array, or nil if absent.
func enumJSON(enum []string) json.RawMessage {
	if enum == nil {
		return nil
	}
	cp := append([]string{}, enum...)
	sort.Strings(cp)
	b, _ := json.Marshal(cp)
	return json.RawMessage(b)
}
