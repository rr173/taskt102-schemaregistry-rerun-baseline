package schema

import (
	"testing"
)

func mustParse(t *testing.T, src string) *Schema {
	t.Helper()
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", src, err)
	}
	return s
}

func TestParseValid(t *testing.T) {
	s := mustParse(t, `{"fields":[
		{"name":"id","type":"integer","required":true},
		{"name":"name","type":"string","required":true},
		{"name":"role","type":"string","enum":["user","admin"],"default":"user"},
		{"name":"score","type":"number","default":0},
		{"name":"active","type":"boolean","default":false}
	]}`)
	if len(s.Fields) != 5 {
		t.Fatalf("expected 5 fields, got %d", len(s.Fields))
	}
	if !s.Fields[4].HasDefault() {
		t.Fatalf("active should have default false")
	}
	if string(s.Fields[4].Default) != "false" {
		t.Fatalf("active default should canonicalize to false, got %s", s.Fields[4].Default)
	}
	if string(s.Fields[3].Default) != "0" {
		t.Fatalf("score default should canonicalize to 0, got %s", s.Fields[3].Default)
	}
}

func TestParseInvalid(t *testing.T) {
	cases := []string{
		`{"fields":[{"name":"","type":"integer"}]}`,                                  // empty name
		`{"fields":[{"name":"1x","type":"integer"}]}`,                                // bad name
		`{"fields":[{"name":"id","type":"int"}]}`,                                    // bad type
		`{"fields":[{"name":"id","type":"integer"},{"name":"id","type":"integer"}]}`, // dup
		`{"fields":[{"name":"r","type":"integer","enum":["a"]}]}`,                    // enum on non-string
		`{"fields":[{"name":"r","type":"string","enum":[]}]}`,                        // empty enum
		`{"fields":[{"name":"r","type":"string","enum":["a","a"]}]}`,                 // dup enum
		`{"fields":[{"name":"id","type":"integer","default":"x"}]}`,                  // default type mismatch
		`{"fields":[{"name":"id","type":"integer","default":1.5}]}`,                  // default not integral
		`{"fields":[{"name":"r","type":"string","enum":["a","b"],"default":"c"}]}`,   // default not in enum
		`{}`, // missing fields
		`not json`,
	}
	for i, c := range cases {
		if _, err := Parse([]byte(c)); err == nil {
			t.Errorf("case %d expected error for %q", i, c)
		}
	}
}

func TestParseEmptySchema(t *testing.T) {
	s := mustParse(t, `{"fields":[]}`)
	if len(s.Fields) != 0 {
		t.Fatalf("expected empty schema")
	}
	if s.Fingerprint() == "" {
		t.Fatalf("empty schema must have a fingerprint")
	}
}

func TestFingerprintOrderIndependent(t *testing.T) {
	a := mustParse(t, `{"fields":[{"name":"id","type":"integer","required":true},{"name":"name","type":"string","required":true}]}`)
	b := mustParse(t, `{"fields":[{"name":"name","type":"string","required":true},{"name":"id","type":"integer","required":true}]}`)
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatalf("fingerprint must be independent of field order: %s vs %s", a.Fingerprint(), b.Fingerprint())
	}
}

func TestFingerprintDefaultZeroValue(t *testing.T) {
	withDefault := mustParse(t, `{"fields":[{"name":"flag","type":"boolean","default":false}]}`)
	withoutDefault := mustParse(t, `{"fields":[{"name":"flag","type":"boolean"}]}`)
	if withDefault.Fingerprint() == withoutDefault.Fingerprint() {
		t.Fatalf("default=false must not collide with no-default")
	}
}

func TestFingerprintEnumOrderIndependent(t *testing.T) {
	a := mustParse(t, `{"fields":[{"name":"r","type":"string","enum":["a","b","c"]}]}`)
	b := mustParse(t, `{"fields":[{"name":"r","type":"string","enum":["c","b","a"]}]}`)
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatalf("fingerprint must be independent of enum order")
	}
}

func TestValidate(t *testing.T) {
	s := mustParse(t, `{"fields":[
		{"name":"id","type":"integer","required":true},
		{"name":"name","type":"string","required":true},
		{"name":"role","type":"string","enum":["user","admin"],"default":"user"}
	]}`)
	cases := []struct {
		name    string
		msg     string
		wantErr bool
		field   string
	}{
		{"valid", `{"id":1,"name":"a","role":"user"}`, false, ""},
		{"valid-unknown-ignored", `{"id":1,"name":"a","extra":1}`, false, ""},
		{"missing-required", `{"name":"a"}`, true, "id"},
		{"type-mismatch", `{"id":"1","name":"a"}`, true, "id"},
		{"integer-fraction", `{"id":1.5,"name":"a"}`, true, "id"},
		{"enum-out-of-range", `{"id":1,"name":"a","role":"superuser"}`, true, "role"},
		{"null-value", `{"id":null,"name":"a"}`, true, "id"},
		{"number-accepts-fraction", `{"id":1,"name":"a","score":1.5}`, false, ""}, // score unknown -> ignored
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := s.Validate([]byte(c.msg))
			if c.wantErr {
				if len(errs) == 0 {
					t.Fatalf("expected error, got none")
				}
				if c.field != "" && errs[0].Field != c.field {
					t.Fatalf("expected error on %s, got %s (%s)", c.field, errs[0].Field, errs[0].Reason)
				}
			} else {
				if len(errs) != 0 {
					t.Fatalf("expected no error, got %+v", errs)
				}
			}
		})
	}
}

func TestValidateNotObject(t *testing.T) {
	s := mustParse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	errs := s.Validate([]byte(`[1,2,3]`))
	if len(errs) == 0 {
		t.Fatalf("expected error for non-object message")
	}
}

func TestDefinitionJSONRoundTrip(t *testing.T) {
	s := mustParse(t, `{"fields":[{"name":"id","type":"integer","required":true,"default":5}]}`)
	b, err := s.DefinitionJSON()
	if err != nil {
		t.Fatal(err)
	}
	s2, err := Parse(b)
	if err != nil {
		t.Fatalf("round-trip parse error: %v", err)
	}
	if s2.Fingerprint() != s.Fingerprint() {
		t.Fatalf("round-trip fingerprint mismatch")
	}
}
