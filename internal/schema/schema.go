// Package schema defines the data-contract (schema) model used by the
// registry: parsing, validation, canonical fingerprinting and message
// validation. A schema is an ordered list of field definitions. Each field
// has a name, a primitive type, a required flag, an optional default value
// and an optional enum (string type only).
package schema

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
)

// FieldType is the set of primitive field types.
type FieldType string

const (
	TypeString  FieldType = "string"
	TypeInteger FieldType = "integer"
	TypeNumber  FieldType = "number"
	TypeBoolean FieldType = "boolean"
)

var validTypes = map[FieldType]bool{
	TypeString:  true,
	TypeInteger: true,
	TypeNumber:  true,
	TypeBoolean: true,
}

var nameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Field is a single field definition. Default is stored as canonical
// (normalized) JSON raw bytes so that semantically equal defaults always share
// the same byte form; an absent default is a nil/empty RawMessage.
type Field struct {
	Name     string          `json:"name"`
	Type     FieldType       `json:"type"`
	Required bool            `json:"required"`
	Default  json.RawMessage `json:"default,omitempty"`
	Enum     []string        `json:"enum,omitempty"`
}

// HasDefault reports whether the field declares a default value.
func (f Field) HasDefault() bool { return len(f.Default) > 0 }

// HasEnum reports whether the field declares an enum constraint. An explicit
// empty enum array counts as present (and is rejected as invalid by Parse).
func (f Field) HasEnum() bool { return f.Enum != nil }

// Schema is an ordered collection of fields.
type Schema struct {
	Fields []Field `json:"fields"`
}

// ByName returns a map from field name to a pointer to the field.
func (s *Schema) ByName() map[string]*Field {
	m := make(map[string]*Field, len(s.Fields))
	for i := range s.Fields {
		m[s.Fields[i].Name] = &s.Fields[i]
	}
	return m
}

// ValidationError describes a single message-validation failure.
type ValidationError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// Parse decodes and validates a schema definition. The returned schema
// preserves the caller's field declaration order and normalizes default values
// to their canonical JSON form.
func Parse(raw []byte) (*Schema, error) {
	var rs struct {
		Fields []Field `json:"fields"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&rs); err != nil {
		return nil, fmt.Errorf("契约定义不是合法 JSON: %w", err)
	}
	if rs.Fields == nil {
		return nil, fmt.Errorf("契约定义缺少 fields 字段")
	}
	seen := make(map[string]bool, len(rs.Fields))
	for i := range rs.Fields {
		f := &rs.Fields[i]
		if err := validateField(f); err != nil {
			return nil, err
		}
		if seen[f.Name] {
			return nil, fmt.Errorf("字段名重复: %s", f.Name)
		}
		seen[f.Name] = true
	}
	return &Schema{Fields: rs.Fields}, nil
}

func validateField(f *Field) error {
	if f.Name == "" {
		return fmt.Errorf("字段名不能为空")
	}
	if !nameRe.MatchString(f.Name) {
		return fmt.Errorf("字段名 %q 不合法，须匹配 ^[A-Za-z_][A-Za-z0-9_]*$", f.Name)
	}
	if !validTypes[f.Type] {
		return fmt.Errorf("字段 %s 的类型 %q 不合法", f.Name, f.Type)
	}
	if f.HasEnum() {
		if f.Type != TypeString {
			return fmt.Errorf("字段 %s 的 enum 仅对 string 类型有效", f.Name)
		}
		if len(f.Enum) == 0 {
			return fmt.Errorf("字段 %s 的 enum 不能为空", f.Name)
		}
		seen := make(map[string]bool, len(f.Enum))
		for _, v := range f.Enum {
			if seen[v] {
				return fmt.Errorf("字段 %s 的 enum 含重复值 %q", f.Name, v)
			}
			seen[v] = true
		}
	}
	if f.HasDefault() {
		norm, err := normalizeDefault(f.Type, f.Default)
		if err != nil {
			return fmt.Errorf("字段 %s 的默认值不合法: %w", f.Name, err)
		}
		f.Default = norm
		if f.HasEnum() {
			var sv string
			if err := json.Unmarshal(norm, &sv); err != nil {
				return fmt.Errorf("字段 %s 的默认值不是字符串", f.Name)
			}
			if !contains(f.Enum, sv) {
				return fmt.Errorf("字段 %s 的默认值 %q 不在 enum 中", f.Name, sv)
			}
		}
	}
	return nil
}

// normalizeDefault parses a raw default value for the given type and returns
// its canonical JSON form so equal values always serialize identically.
func normalizeDefault(t FieldType, raw json.RawMessage) (json.RawMessage, error) {
	v, err := decodeValue(raw)
	if err != nil {
		return nil, err
	}
	switch t {
	case TypeInteger:
		n, ok := v.(json.Number)
		if !ok {
			return nil, fmt.Errorf("期望整数")
		}
		i, ok := exactInteger(n)
		if !ok {
			return nil, fmt.Errorf("期望整数，得到小数或超出 int64 范围")
		}
		return json.Marshal(i)
	case TypeNumber:
		n, ok := v.(json.Number)
		if !ok {
			return nil, fmt.Errorf("期望数字")
		}
		f, err := n.Float64()
		if err != nil {
			return nil, fmt.Errorf("期望数字: %w", err)
		}
		return json.Marshal(f)
	case TypeString:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("期望字符串")
		}
		return json.Marshal(s)
	case TypeBoolean:
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("期望布尔")
		}
		return json.Marshal(b)
	}
	return nil, fmt.Errorf("未知类型 %s", t)
}

// decodeValue decodes raw JSON using json.Number so number subtypes are
// distinguishable. Object and array values are rejected as invalid scalars.
func decodeValue(raw json.RawMessage) (interface{}, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v interface{}
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	switch v.(type) {
	case json.Number, string, bool, nil:
		return v, nil
	default:
		return nil, fmt.Errorf("不支持的字面量类型")
	}
}

// DefinitionJSON returns the schema serialized in its original field order.
func (s *Schema) DefinitionJSON() ([]byte, error) {
	return json.Marshal(struct {
		Fields []Field `json:"fields"`
	}{Fields: s.Fields})
}

// Fingerprint returns a stable hex SHA-256 digest of the schema's canonical
// form: fields sorted by name, enum values sorted, defaults in canonical form.
// It is independent of field declaration order.
func (s *Schema) Fingerprint() string {
	fields := make([]Field, len(s.Fields))
	copy(fields, s.Fields)
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	enc := make([]map[string]interface{}, 0, len(fields))
	for _, f := range fields {
		m := map[string]interface{}{
			"name":     f.Name,
			"type":     string(f.Type),
			"required": f.Required,
		}
		if f.HasDefault() {
			m["default"] = json.RawMessage(f.Default)
		}
		if f.HasEnum() {
			enum := append([]string{}, f.Enum...)
			sort.Strings(enum)
			m["enum"] = enum
		}
		enc = append(enc, m)
	}
	b, err := json.Marshal(map[string]interface{}{"fields": enc})
	if err != nil {
		// marshaling of primitive maps cannot fail in practice
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Validate checks a JSON message against the schema and returns all errors.
// Unknown fields are ignored. Required fields must be present; present fields
// must match their declared type and enum.
func (s *Schema) Validate(msg []byte) []ValidationError {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(msg, &m); err != nil {
		return []ValidationError{{Field: "", Reason: "消息不是 JSON 对象"}}
	}
	if m == nil {
		return []ValidationError{{Field: "", Reason: "消息不是 JSON 对象"}}
	}
	var errs []ValidationError
	for _, f := range s.Fields {
		raw, ok := m[f.Name]
		if !ok {
			if f.Required {
				errs = append(errs, ValidationError{Field: f.Name, Reason: "缺少必填字段"})
			}
			continue
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			errs = append(errs, ValidationError{Field: f.Name, Reason: "字段值为 null"})
			continue
		}
		v, err := decodeValue(raw)
		if err != nil {
			errs = append(errs, ValidationError{Field: f.Name, Reason: typeReason(f.Type)})
			continue
		}
		if !matchType(f.Type, v) {
			errs = append(errs, ValidationError{Field: f.Name, Reason: typeReason(f.Type)})
			continue
		}
		if f.HasEnum() && f.Type == TypeString {
			sv := v.(string)
			if !contains(f.Enum, sv) {
				errs = append(errs, ValidationError{Field: f.Name, Reason: "枚举越界"})
			}
		}
	}
	return errs
}

func matchType(t FieldType, v interface{}) bool {
	switch t {
	case TypeInteger:
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		_, ok = exactInteger(n)
		return ok
	case TypeNumber:
		_, ok := v.(json.Number)
		return ok
	case TypeString:
		_, ok := v.(string)
		return ok
	case TypeBoolean:
		_, ok := v.(bool)
		return ok
	}
	return false
}

// exactInteger converts a JSON number to int64 without passing through
// float64, which would lose precision for values above 2^53.
func exactInteger(n json.Number) (int64, bool) {
	f, err := n.Float64()
	if err != nil || math.IsInf(f, 0) || math.Trunc(f) != f {
		return 0, false
	}
	return int64(f), true
}

func typeReason(t FieldType) string {
	switch t {
	case TypeInteger:
		return "期望 integer"
	case TypeNumber:
		return "期望 number"
	case TypeString:
		return "期望 string"
	case TypeBoolean:
		return "期望 boolean"
	}
	return "类型不符"
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// Canonicalize returns a copy of the message that conforms to the schema:
// fields not declared in the schema are removed, and absent optional fields
// that declare a default are filled with that default. Present fields are kept
// verbatim. If any present field violates its declared type or enum, or a
// required field is absent, the violations are returned and no canonicalized
// message is produced. The output object keys are sorted (deterministic).
func (s *Schema) Canonicalize(msg []byte) (json.RawMessage, []ValidationError) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(msg, &m); err != nil {
		return nil, []ValidationError{{Field: "", Reason: "消息不是 JSON 对象"}}
	}
	if m == nil {
		return nil, []ValidationError{{Field: "", Reason: "消息不是 JSON 对象"}}
	}
	if errs := s.Validate(msg); len(errs) > 0 {
		return nil, errs
	}
	out := make(map[string]json.RawMessage, len(s.Fields))
	for _, f := range s.Fields {
		if raw, ok := m[f.Name]; ok {
			out[f.Name] = raw
		} else if f.HasDefault() && string(f.Default) != "false" {
			out[f.Name] = f.Default
		}
		// absent optional field without default: omitted
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, []ValidationError{{Field: "", Reason: "序列化失败"}}
	}
	return json.RawMessage(b), nil
}

// FieldCount returns the number of declared fields.
func (s *Schema) FieldCount() int { return len(s.Fields) }

// RequiredFields returns the names of required fields in declaration order.
func (s *Schema) RequiredFields() []string {
	var out []string
	for _, f := range s.Fields {
		if f.Required {
			out = append(out, f.Name)
		}
	}
	return out
}

// Summary returns a compact, human-readable one-line description of the schema.
func (s *Schema) Summary() string {
	if len(s.Fields) == 0 {
		return "empty schema (0 fields)"
	}
	req := s.RequiredFields()
	return fmt.Sprintf("%d fields, %d required (%s)", len(s.Fields), len(req), joinNames(req))
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}
