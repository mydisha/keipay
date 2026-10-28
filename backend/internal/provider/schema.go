package provider

import (
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mydisha/keipay/backend/internal/domain"
)

// FieldType is the value type of a schema field.
type FieldType string

// Field types. Credential fields are always strings.
const (
	TypeString  FieldType = "string"
	TypeInteger FieldType = "integer"
	TypeBoolean FieldType = "boolean"
	TypeEnum    FieldType = "enum"
)

// Option is one value of an enum field.
type Option struct {
	Value string
	Label string
}

// Field is one credential or setting, in display order. Validation keywords
// mirror JSON Schema; the console renders forms from the same data.
type Field struct {
	Key         string
	Label       string
	Description string
	Placeholder string
	Type        FieldType
	Secret      bool
	Required    bool
	// Default is used when the value is omitted; nil means none. Secret
	// fields never have one.
	Default any
	// Options lists the values of an enum field.
	Options []Option
	// MinLength and MaxLength count code points.
	MinLength *int
	MaxLength *int
	// Pattern is an RE2 expression the whole value must match; PatternHint
	// says the same for people.
	Pattern     string
	PatternHint string
	Minimum     *int64
	Maximum     *int64
}

// Schema is an ordered list of fields.
type Schema struct {
	Fields []Field
}

// Validation rules reported as domain.FieldError.Rule.
const (
	RuleRequired  = "required"
	RuleType      = "type"
	RuleMinLength = "min_length"
	RuleMaxLength = "max_length"
	RulePattern   = "pattern"
	RuleMinimum   = "minimum"
	RuleMaximum   = "maximum"
	RuleOneOf     = "oneof"
	RuleUnknown   = "unknown"
)

// IntPtr and Int64Ptr help adapters write schema bounds.
func IntPtr(v int) *int { return &v }

// Int64Ptr returns a pointer to v.
func Int64Ptr(v int64) *int64 { return &v }

// ValidateValues checks in against s and returns the cleaned values: strings
// trimmed, integers as int64, omitted fields with a default filled in and
// omitted fields without one left out. A value that is missing, null or a
// blank string counts as omitted. Errors name "<prefix>.<key>", in schema
// order with keys the schema does not define last (alphabetical), and at
// most one per field.
func ValidateValues(s Schema, in map[string]any, prefix string) (map[string]any, []domain.FieldError) {
	out := make(map[string]any, len(s.Fields))
	var errs []domain.FieldError
	known := make(map[string]bool, len(s.Fields))
	for _, f := range s.Fields {
		known[f.Key] = true
		path := prefix + "." + f.Key
		v, present := in[f.Key]
		if str, ok := v.(string); ok {
			v = strings.TrimSpace(str)
		}
		if !present || v == nil || v == "" {
			if f.Required {
				errs = append(errs, domain.FieldError{Field: path, Rule: RuleRequired})
			} else if f.Default != nil {
				out[f.Key] = f.Default
			}
			continue
		}
		val, rule := f.check(v)
		if rule != "" {
			errs = append(errs, domain.FieldError{Field: path, Rule: rule})
			continue
		}
		out[f.Key] = val
	}
	var unknown []string
	for k := range in {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	slices.Sort(unknown)
	for _, k := range unknown {
		errs = append(errs, domain.FieldError{Field: prefix + "." + k, Rule: RuleUnknown})
	}
	return out, errs
}

// check validates one present, non-blank value and returns it in its stored
// form, or the failed rule.
func (f Field) check(v any) (any, string) {
	switch f.Type {
	case TypeString:
		s, ok := v.(string)
		if !ok {
			return nil, RuleType
		}
		n := utf8.RuneCountInString(s)
		if f.MinLength != nil && n < *f.MinLength {
			return nil, RuleMinLength
		}
		if f.MaxLength != nil && n > *f.MaxLength {
			return nil, RuleMaxLength
		}
		if f.Pattern != "" && !fullMatch(f.Pattern, s) {
			return nil, RulePattern
		}
		return s, ""
	case TypeInteger:
		n, ok := toInt64(v)
		if !ok {
			return nil, RuleType
		}
		if f.Minimum != nil && n < *f.Minimum {
			return nil, RuleMinimum
		}
		if f.Maximum != nil && n > *f.Maximum {
			return nil, RuleMaximum
		}
		return n, ""
	case TypeBoolean:
		b, ok := v.(bool)
		if !ok {
			return nil, RuleType
		}
		return b, ""
	case TypeEnum:
		s, ok := v.(string)
		if !ok {
			return nil, RuleType
		}
		if !slices.ContainsFunc(f.Options, func(o Option) bool { return o.Value == s }) {
			return nil, RuleOneOf
		}
		return s, ""
	default:
		return nil, RuleType
	}
}

// fullMatch reports whether the whole of s matches pattern. A pattern that
// does not compile matches nothing; adapter tests compile every pattern.
func fullMatch(pattern, s string) bool {
	re, err := regexp.Compile(`^(?:` + pattern + `)$`)
	return err == nil && re.MatchString(s)
}

// toInt64 accepts whole numbers only: a JSON number written without a
// fraction or exponent, or a Go integer.
func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case json.Number:
		i, err := strconv.ParseInt(string(n), 10, 64)
		return i, err == nil
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		if n != math.Trunc(n) || n < math.MinInt64 || n >= math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	default:
		return 0, false
	}
}

// IntSetting reads an integer setting stored by ValidateValues or decoded
// from JSON with json.Number; ok is false when it is absent.
func IntSetting(s Settings, key string) (int64, bool) {
	v, present := s[key]
	if !present || v == nil {
		return 0, false
	}
	return toInt64(v)
}

// StringSetting reads a string or enum setting; "" when absent.
func StringSetting(s Settings, key string) string {
	v, _ := s[key].(string)
	return v
}
