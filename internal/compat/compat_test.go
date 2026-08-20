package compat

import (
	"testing"

	"task102-schemaregistry/internal/schema"
)

func parse(t *testing.T, src string) *schema.Schema {
	t.Helper()
	s, err := schema.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return s
}

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{
		"":         Backward,
		"BACKWARD": Backward,
		"FORWARD":  Forward,
		"FULL":     Full,
		"NONE":     None,
	}
	for in, want := range cases {
		got, err := ParseMode(in)
		if err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q,%v want %q", in, got, err, want)
		}
	}
	if _, err := ParseMode("WEIRD"); err == nil {
		t.Fatalf("expected error for unknown mode")
	}
}

func TestBackwardAddOptionalOrDefault(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	// add optional no default -> OK
	if ok, _ := Check(Backward, old, parse(t, `{"fields":[
		{"name":"id","type":"integer","required":true},
		{"name":"email","type":"string","required":false}
	]}`)); !ok {
		t.Fatalf("add optional field should be backward-compatible")
	}
	// add optional with default -> OK
	if ok, _ := Check(Backward, old, parse(t, `{"fields":[
		{"name":"id","type":"integer","required":true},
		{"name":"email","type":"string","required":false,"default":""}
	]}`)); !ok {
		t.Fatalf("add optional field with default should be backward-compatible")
	}
}

func TestBackwardAddRequiredNoDefaultFails(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	newer := parse(t, `{"fields":[
		{"name":"id","type":"integer","required":true},
		{"name":"age","type":"integer","required":true}
	]}`)
	ok, v := Check(Backward, old, newer)
	if ok {
		t.Fatalf("add required no-default should fail backward")
	}
	if v.Field != "age" {
		t.Fatalf("expected violation on age, got %s", v.Field)
	}
}

func TestBackwardDeleteFieldOK(t *testing.T) {
	old := parse(t, `{"fields":[
		{"name":"id","type":"integer","required":true},
		{"name":"name","type":"string","required":true}
	]}`)
	newer := parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	if ok, _ := Check(Backward, old, newer); !ok {
		t.Fatalf("deleting a field is backward-compatible (new ignores extra)")
	}
}

func TestBackwardTypeChangeFails(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	newer := parse(t, `{"fields":[{"name":"id","type":"string","required":true}]}`)
	ok, v := Check(Backward, old, newer)
	if ok || v.Field != "id" {
		t.Fatalf("type change should fail backward on id, got ok=%v v=%+v", ok, v)
	}
}

func TestBackwardOptionalToRequired(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"id","type":"integer","required":false}]}`)
	// optional -> required without default -> fail
	ok, v := Check(Backward, old, parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`))
	if ok || v.Field != "id" {
		t.Fatalf("optional->required no default should fail")
	}
	// optional -> required with default -> OK
	if ok, _ := Check(Backward, old, parse(t, `{"fields":[{"name":"id","type":"integer","required":true,"default":0}]}`)); !ok {
		t.Fatalf("optional->required with default should be backward-compatible")
	}
}

func TestBackwardEnum(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"r","type":"string","enum":["a","b"]}]}`)
	// superset -> OK
	if ok, _ := Check(Backward, old, parse(t, `{"fields":[{"name":"r","type":"string","enum":["a","b","c"]}]}`)); !ok {
		t.Fatalf("enum superset should be backward-compatible")
	}
	// narrowed -> fail
	if ok, v := Check(Backward, old, parse(t, `{"fields":[{"name":"r","type":"string","enum":["a"]}]}`)); ok || v.Field != "r" {
		t.Fatalf("enum narrow should fail backward on r")
	}
	// add enum to non-enum field -> fail
	// none -> enum is narrowing
	old2 := parse(t, `{"fields":[{"name":"r","type":"string"}]}`)
	if ok, v := Check(Backward, old2, parse(t, `{"fields":[{"name":"r","type":"string","enum":["a"]}]}`)); ok || v.Field != "r" {
		t.Fatalf("adding enum constraint should fail backward")
	}
}

func TestForwardDeleteRequiredNoDefaultFails(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"k","type":"string","required":true}]}`)
	newer := parse(t, `{"fields":[]}`)
	ok, v := Check(Forward, old, newer)
	if ok || v.Field != "k" {
		t.Fatalf("deleting required no-default should fail forward on k, got %+v", v)
	}
}

func TestForwardDeleteOptionalOK(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"k","type":"string","required":false}]}`)
	if ok, _ := Check(Forward, old, parse(t, `{"fields":[]}`)); !ok {
		t.Fatalf("deleting optional field is forward-compatible")
	}
}

func TestForwardAddFieldOK(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	newer := parse(t, `{"fields":[
		{"name":"id","type":"integer","required":true},
		{"name":"age","type":"integer","required":true}
	]}`)
	if ok, _ := Check(Forward, old, newer); !ok {
		t.Fatalf("adding a field is forward-compatible (old ignores extra)")
	}
}

func TestForwardRequiredToOptional(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	// required -> optional without old default -> fail
	ok, v := Check(Forward, old, parse(t, `{"fields":[{"name":"id","type":"integer","required":false}]}`))
	if ok || v.Field != "id" {
		t.Fatalf("required->optional no old default should fail forward")
	}
	// required -> optional with old default -> OK
	oldWithDefault := parse(t, `{"fields":[{"name":"id","type":"integer","required":true,"default":0}]}`)
	if ok, _ := Check(Forward, oldWithDefault, parse(t, `{"fields":[{"name":"id","type":"integer","required":false}]}`)); !ok {
		t.Fatalf("required->optional with old default should be forward-compatible")
	}
}

func TestForwardEnum(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"r","type":"string","enum":["a","b"]}]}`)
	// subset -> OK
	if ok, _ := Check(Forward, old, parse(t, `{"fields":[{"name":"r","type":"string","enum":["a"]}]}`)); !ok {
		t.Fatalf("enum subset should be forward-compatible")
	}
	// expanded -> fail
	if ok, v := Check(Forward, old, parse(t, `{"fields":[{"name":"r","type":"string","enum":["a","b","c"]}]}`)); ok || v.Field != "r" {
		t.Fatalf("enum expand should fail forward")
	}
	// remove enum -> fail (new can produce values outside old enum)
	if ok, v := Check(Forward, old, parse(t, `{"fields":[{"name":"r","type":"string"}]}`)); ok || v.Field != "r" {
		t.Fatalf("removing enum should fail forward")
	}
	// old no enum, new adds enum -> OK
	old2 := parse(t, `{"fields":[{"name":"r","type":"string"}]}`)
	if ok, _ := Check(Forward, old2, parse(t, `{"fields":[{"name":"r","type":"string","enum":["a"]}]}`)); !ok {
		t.Fatalf("old no enum + new enum should be forward-compatible")
	}
}

func TestFull(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	// type change fails both -> full fails
	if ok, _ := Check(Full, old, parse(t, `{"fields":[{"name":"id","type":"string","required":true}]}`)); ok {
		t.Fatalf("type change should fail full")
	}
	// add optional field: backward OK (optional), forward OK (add) -> full OK
	if ok, _ := Check(Full, old, parse(t, `{"fields":[
		{"name":"id","type":"integer","required":true},
		{"name":"e","type":"string","required":false}
	]}`)); !ok {
		t.Fatalf("add optional should be full-compatible")
	}
}

func TestNoneSkipsCheck(t *testing.T) {
	old := parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	newer := parse(t, `{"fields":[{"name":"id","type":"string","required":true}]}`)
	if ok, _ := Check(None, old, newer); !ok {
		t.Fatalf("NONE should skip all checks")
	}
}

func TestNilOld(t *testing.T) {
	newer := parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	if ok, _ := Check(Backward, nil, newer); !ok {
		t.Fatalf("nil old (first version) should always be compatible")
	}
}

func TestParseModeTransitive(t *testing.T) {
	for _, m := range []Mode{BackwardTransitive, ForwardTransitive, FullTransitive} {
		got, err := ParseMode(string(m))
		if err != nil || got != m {
			t.Errorf("ParseMode(%q) = %q,%v want %q", m, got, err, m)
		}
	}
	if !IsTransitive(BackwardTransitive) {
		t.Fatalf("BACKWARD_TRANSITIVE should be transitive")
	}
	if IsTransitive(Backward) {
		t.Fatalf("BACKWARD should not be transitive")
	}
}

func TestCheckAgainstNonTransitiveUsesLatest(t *testing.T) {
	// v1: id required. v2: id required + name optional. New adds required age.
	// Non-transitive BACKWARD compares only against v2 (latest), which is
	// compatible with adding an optional field, so adding age must still fail
	// because age is required-no-default — fails against v2 too.
	v1 := parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	v2 := parse(t, `{"fields":[{"name":"id","type":"integer","required":true},{"name":"name","type":"string","required":false}]}`)
	newer := parse(t, `{"fields":[{"name":"id","type":"integer","required":true},{"name":"name","type":"string","required":false},{"name":"age","type":"integer","required":true}]}`)
	olds := []VersionedSchema{{1, v1}, {2, v2}}
	ok, v := CheckAgainst(Backward, olds, newer)
	if ok || v.Field != "age" {
		t.Fatalf("non-transitive should fail on age against latest v2, got ok=%v v=%+v", ok, v)
	}
	if v.Version != 2 {
		t.Fatalf("violation should carry latest version 2, got %d", v.Version)
	}
}

func TestCheckAgainstTransitiveReportsFailingVersion(t *testing.T) {
	// v1: id required string. v2: id required integer (allowed only under NONE).
	// Build v1 and v2 directly. Under BACKWARD_TRANSITIVE, a candidate that is
	// backward-compatible with v2 but not v1 should fail with Version=1.
	v1 := parse(t, `{"fields":[{"name":"id","type":"string","required":true}]}`)
	v2 := parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	// candidate == v2: backward-compatible with v2 (identical), but v2 itself is
	// NOT backward-compatible with v1 (type changed string->integer). So
	// registering v2 again is fine vs v2, but transitive checks vs v1 fail.
	cand := parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	olds := []VersionedSchema{{1, v1}, {2, v2}}
	ok, v := CheckAgainst(BackwardTransitive, olds, cand)
	if ok {
		t.Fatalf("transitive should fail because cand is not backward-compatible with v1")
	}
	if v.Version != 1 {
		t.Fatalf("failing version should be 1, got %d", v.Version)
	}
}

func TestCheckAgainstTransitivePassesWhenCompatibleWithAll(t *testing.T) {
	v1 := parse(t, `{"fields":[{"name":"id","type":"integer","required":true}]}`)
	v2 := parse(t, `{"fields":[{"name":"id","type":"integer","required":true},{"name":"name","type":"string","required":false}]}`)
	cand := parse(t, `{"fields":[{"name":"id","type":"integer","required":true},{"name":"name","type":"string","required":false},{"name":"email","type":"string","required":false,"default":""}]}`)
	olds := []VersionedSchema{{1, v1}, {2, v2}}
	if ok, _ := CheckAgainst(BackwardTransitive, olds, cand); !ok {
		t.Fatalf("candidate adding optional fields should be backward-transitive compatible with all")
	}
}
