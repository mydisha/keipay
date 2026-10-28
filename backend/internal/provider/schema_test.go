package provider_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/provider"
)

var testSchema = provider.Schema{Fields: []provider.Field{
	{Key: "token", Type: provider.TypeString, Required: true, MinLength: provider.IntPtr(3), MaxLength: provider.IntPtr(5)},
	{Key: "code", Type: provider.TypeString, Pattern: `[a-z]+_[0-9]+`},
	{Key: "lifetime", Type: provider.TypeInteger, Minimum: provider.Int64Ptr(60), Maximum: provider.Int64Ptr(120)},
	{Key: "retries", Type: provider.TypeInteger, Default: int64(3)},
	{Key: "sandbox", Type: provider.TypeBoolean, Default: true},
	{Key: "locale", Type: provider.TypeEnum, Options: []provider.Option{{Value: "id"}, {Value: "en"}}},
	{Key: "mystery", Type: provider.FieldType("color")},
}}

func fe(field, rule string) domain.FieldError { return domain.FieldError{Field: field, Rule: rule} }

// T-1 / AC-4
func TestValidateValues(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want map[string]any
		errs []domain.FieldError
	}{
		{"defaults and trimming", `{"token":"  abc  "}`,
			map[string]any{"token": "abc", "retries": int64(3), "sandbox": true}, nil},
		{"everything valid", `{"token":"abcde","code":"ab_12","lifetime":60,"retries":0,"sandbox":false,"locale":"en"}`,
			map[string]any{"token": "abcde", "code": "ab_12", "lifetime": int64(60), "retries": int64(0), "sandbox": false, "locale": "en"}, nil},
		{"required: missing", `{}`, nil, []domain.FieldError{fe("s.token", "required")}},
		{"required: null", `{"token":null}`, nil, []domain.FieldError{fe("s.token", "required")}},
		{"required: blank", `{"token":"   "}`, nil, []domain.FieldError{fe("s.token", "required")}},
		{"min_length", `{"token":"ab"}`, nil, []domain.FieldError{fe("s.token", "min_length")}},
		{"max_length", `{"token":"abcdef"}`, nil, []domain.FieldError{fe("s.token", "max_length")}},
		{"code points, not bytes", `{"token":"ñññññ"}`, map[string]any{"token": "ñññññ", "retries": int64(3), "sandbox": true}, nil},
		{"code points over", `{"token":"ññññññ"}`, nil, []domain.FieldError{fe("s.token", "max_length")}},
		{"pattern is anchored", `{"token":"abc","code":"xab_12y!"}`, nil, []domain.FieldError{fe("s.code", "pattern")}},
		{"type: string", `{"token":123}`, nil, []domain.FieldError{fe("s.token", "type")}},
		{"type: float", `{"token":"abc","lifetime":1.5}`, nil, []domain.FieldError{fe("s.lifetime", "type")}},
		{"type: quoted number", `{"token":"abc","lifetime":"60"}`, nil, []domain.FieldError{fe("s.lifetime", "type")}},
		{"type: beyond int64", `{"token":"abc","lifetime":9223372036854775808}`, nil, []domain.FieldError{fe("s.lifetime", "type")}},
		{"minimum", `{"token":"abc","lifetime":59}`, nil, []domain.FieldError{fe("s.lifetime", "minimum")}},
		{"maximum", `{"token":"abc","lifetime":121}`, nil, []domain.FieldError{fe("s.lifetime", "maximum")}},
		{"type: boolean", `{"token":"abc","sandbox":"yes"}`, nil, []domain.FieldError{fe("s.sandbox", "type")}},
		{"oneof", `{"token":"abc","locale":"fr"}`, nil, []domain.FieldError{fe("s.locale", "oneof")}},
		{"type: enum", `{"token":"abc","locale":1}`, nil, []domain.FieldError{fe("s.locale", "type")}},
		{"unknown field type", `{"token":"abc","mystery":"red"}`, nil, []domain.FieldError{fe("s.mystery", "type")}},
		{"stable order, unknown keys last", `{"zeta":1,"locale":"xx","alpha":2,"lifetime":5}`, nil, []domain.FieldError{
			fe("s.token", "required"), fe("s.lifetime", "minimum"), fe("s.locale", "oneof"), fe("s.alpha", "unknown"), fe("s.zeta", "unknown")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := json.NewDecoder(strings.NewReader(tc.in))
			dec.UseNumber()
			var in map[string]any
			if err := dec.Decode(&in); err != nil {
				t.Fatal(err)
			}
			got, errs := provider.ValidateValues(testSchema, in, "s")
			if !reflect.DeepEqual(errs, tc.errs) {
				t.Fatalf("errors = %v, want %v", errs, tc.errs)
			}
			if tc.want != nil && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("values = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestValidateValuesNilInputFillsDefaults(t *testing.T) {
	s := provider.Schema{Fields: []provider.Field{{Key: "locale", Type: provider.TypeEnum, Default: "en",
		Options: []provider.Option{{Value: "en"}}}, {Key: "lifetime", Type: provider.TypeInteger}}}
	got, errs := provider.ValidateValues(s, nil, "settings")
	if errs != nil || !reflect.DeepEqual(got, map[string]any{"locale": "en"}) {
		t.Fatalf("got %v %v", got, errs)
	}
}

func TestSettingReaders(t *testing.T) {
	s := provider.Settings{"a": json.Number("300"), "b": int64(5), "c": "id", "d": nil}
	if v, ok := provider.IntSetting(s, "a"); !ok || v != 300 {
		t.Fatalf("a = %d %v", v, ok)
	}
	if v, ok := provider.IntSetting(s, "b"); !ok || v != 5 {
		t.Fatalf("b = %d %v", v, ok)
	}
	if _, ok := provider.IntSetting(s, "d"); ok {
		t.Fatal("nil read as a number")
	}
	if provider.StringSetting(s, "c") != "id" || provider.StringSetting(s, "missing") != "" {
		t.Fatal("StringSetting")
	}
}
