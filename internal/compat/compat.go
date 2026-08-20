// Package compat implements schema compatibility checking between versions of
// a subject's data contract. A compatibility mode declares the direction in
// which data must remain consumable after the schema evolves:
//
//   - BACKWARD  — new schema can read data written with the latest old version
//   - FORWARD   — old schema can read data written with the new version
//   - FULL      — both BACKWARD and FORWARD against the latest old version
//   - BACKWARD_TRANSITIVE / FORWARD_TRANSITIVE / FULL_TRANSITIVE — same check
//     against every prior version, not just the latest
//   - NONE      — skip the check entirely
//
// A check reports the first violating field (by deterministic field order) so a
// caller can surface a single, actionable reason.
package compat

import (
	"fmt"
	"sort"
	"strings"

	"task102-schemaregistry/internal/schema"
)

// Mode is a compatibility policy.
type Mode string

const (
	Backward           Mode = "BACKWARD"
	Forward            Mode = "FORWARD"
	Full               Mode = "FULL"
	None               Mode = "NONE"
	BackwardTransitive Mode = "BACKWARD_TRANSITIVE"
	ForwardTransitive  Mode = "FORWARD_TRANSITIVE"
	FullTransitive     Mode = "FULL_TRANSITIVE"
)

// ParseMode parses a mode string. The empty string defaults to Backward. The
// transitive variants are accepted in addition to the base four.
func ParseMode(s string) (Mode, error) {
	if s == "" {
		return Backward, nil
	}
	switch Mode(s) {
	case Backward, Forward, Full, None,
		BackwardTransitive, ForwardTransitive, FullTransitive:
		return Mode(s), nil
	}
	return "", fmt.Errorf("未知兼容性模式 %q（支持 BACKWARD / FORWARD / FULL / NONE 及其 _TRANSITIVE 变体）", s)
}

// IsTransitive reports whether the mode checks against every prior version.
func IsTransitive(m Mode) bool {
	return strings.HasSuffix(string(m), "_TRANSITIVE")
}

// IsNone reports whether the mode skips compatibility checking.
func IsNone(m Mode) bool { return m == None }

// baseMode strips a _TRANSITIVE suffix, returning the underlying direction.
func baseMode(m Mode) Mode {
	return Mode(strings.TrimSuffix(string(m), "_TRANSITIVE"))
}

// Violation names the first field and reason a compatibility check failed. For
// transitive checks, Version identifies which prior version failed.
type Violation struct {
	Field   string `json:"field,omitempty"`
	Reason  string `json:"reason"`
	Version int    `json:"version,omitempty"`
}

func (v Violation) Error() string {
	if v.Field == "" {
		if v.Version > 0 {
			return fmt.Sprintf("版本 %d: %s", v.Version, v.Reason)
		}
		return v.Reason
	}
	if v.Version > 0 {
		return fmt.Sprintf("版本 %d 字段 %s: %s", v.Version, v.Field, v.Reason)
	}
	return fmt.Sprintf("字段 %s: %s", v.Field, v.Reason)
}

// VersionedSchema pairs a schema with the version number it was registered as.
type VersionedSchema struct {
	Version int
	Schema  *schema.Schema
}

// Check reports whether new is compatible with old under the given mode, where
// old is the single latest prior version. It is the non-transitive entry point.
// A nil old (no prior version) is always compatible.
func Check(mode Mode, old, newer *schema.Schema) (bool, Violation) {
	if mode == None || old == nil {
		return true, Violation{}
	}
	return checkOne(mode, old, newer)
}

// CheckAgainst reports whether newer is compatible under mode against the list
// of prior versions (ordered ascending). For non-transitive modes only the
// latest (last) prior version is consulted; for transitive modes every prior
// version is consulted and the first violation carries that version's number.
// An empty prior list is always compatible.
func CheckAgainst(mode Mode, olds []VersionedSchema, newer *schema.Schema) (bool, Violation) {
	if mode == None || len(olds) == 0 {
		return true, Violation{}
	}
	if !IsTransitive(mode) {
		latest := olds[len(olds)-1]
		ok, v := checkOne(mode, latest.Schema, newer)
		if !ok {
			v.Version = latest.Version
		}
		return ok, v
	}
	base := baseMode(mode)
	for _, ov := range olds {
		if ok, v := checkOne(base, ov.Schema, newer); !ok {
			v.Version = ov.Version
			return false, v
		}
	}
	return true, Violation{}
}

// checkOne dispatches a single (non-transitive) check.
func checkOne(mode Mode, old, newer *schema.Schema) (bool, Violation) {
	switch mode {
	case Backward:
		return backward(old, newer)
	case Forward:
		return forward(old, newer)
	case Full:
		if ok, v := backward(old, newer); !ok {
			return false, v
		}
		return forward(old, newer)
	case None:
		return true, Violation{}
	}
	return false, Violation{Reason: fmt.Sprintf("未知兼容性模式 %q", mode)}
}

// backward: new reads old data.
func backward(old, newer *schema.Schema) (bool, Violation) {
	oldMap := old.ByName()
	newMap := newer.ByName()

	// Common fields first (sorted), then added fields. Removed fields are
	// always backward-compatible (new ignores extra data in old records).
	for _, name := range sortedCommon(oldMap, newMap) {
		o := oldMap[name]
		n := newMap[name]
		if o.Type != n.Type {
			return false, Violation{name, fmt.Sprintf("字段类型由 %s 变为 %s", o.Type, n.Type), 0}
		}
		if !o.Required && n.Required && !n.HasDefault() {
			return false, Violation{name, "字段由可选改为必填且无默认值", 0}
		}
		if ok, v := enumBackwardViolation(name, o, n); !ok {
			return false, v
		}
	}
	for _, name := range sortedAdded(newMap, oldMap) {
		n := newMap[name]
		if n.Required && !n.HasDefault() {
			return false, Violation{name, "新增必填字段无默认值，旧数据缺该字段", 0}
		}
	}
	return true, Violation{}
}

// forward: old reads new data.
func forward(old, newer *schema.Schema) (bool, Violation) {
	oldMap := old.ByName()
	newMap := newer.ByName()

	// Common fields first (sorted), then removed fields. Added fields are
	// always forward-compatible (old ignores extra data in new records).
	for _, name := range sortedCommon(oldMap, newMap) {
		o := oldMap[name]
		n := newMap[name]
		if o.Type != n.Type {
			return false, Violation{name, fmt.Sprintf("字段类型由 %s 变为 %s", o.Type, n.Type), 0}
		}
		if o.Required && !n.Required && !o.HasDefault() {
			return false, Violation{name, "字段由必填改为可选且旧契约无默认值", 0}
		}
		if ok, v := enumForwardViolation(name, o, n); !ok {
			return false, v
		}
	}
	for _, name := range sortedRemoved(oldMap, newMap) {
		o := oldMap[name]
		if o.Required && !o.HasDefault() {
			return false, Violation{name, "删除必填字段且无默认值，新数据缺该字段", 0}
		}
	}
	return true, Violation{}
}

// enumBackwardViolation: new enum must accept all old values.
func enumBackwardViolation(name string, o, n *schema.Field) (bool, Violation) {
	switch {
	case o.HasEnum() && n.HasEnum():
		if !superset(n.Enum, o.Enum) {
			return false, Violation{name, "字段枚举集合收窄", 0}
		}
	case !o.HasEnum() && n.HasEnum():
		return false, Violation{name, "字段新增枚举约束，旧数据可能越界", 0}
	}
	return true, Violation{}
}

// enumForwardViolation: old enum must accept all new values.
func enumForwardViolation(name string, o, n *schema.Field) (bool, Violation) {
	switch {
	case o.HasEnum() && n.HasEnum():
		if !superset(o.Enum, n.Enum) {
			return false, Violation{name, "字段枚举集合扩展超出旧契约", 0}
		}
	case o.HasEnum() && !n.HasEnum():
		return false, Violation{name, "字段移除枚举约束，新数据可能越出旧契约枚举", 0}
	}
	return true, Violation{}
}

// superset reports whether every element of small is in big.
func superset(big, small []string) bool {
	set := make(map[string]bool, len(big))
	for _, v := range big {
		set[v] = true
	}
	for _, v := range small {
		if !set[v] {
			return false
		}
	}
	return true
}

func sortedCommon(a, b map[string]*schema.Field) []string {
	var out []string
	for name := range a {
		if _, ok := b[name]; ok {
			out = append(out, name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

func sortedAdded(newer, older map[string]*schema.Field) []string {
	var out []string
	for name := range newer {
		if _, ok := older[name]; !ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func sortedRemoved(older, newer map[string]*schema.Field) []string {
	var out []string
	for name := range older {
		if _, ok := newer[name]; !ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
