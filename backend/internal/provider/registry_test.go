package provider_test

import (
	"errors"
	"testing"

	"github.com/mydisha/keipay/backend/internal/provider"
	"github.com/mydisha/keipay/backend/internal/provider/providertest"
	"github.com/mydisha/keipay/backend/internal/provider/xendit"
)

func named(n provider.Name) *providertest.Fake {
	f := providertest.New()
	f.Desc.Name = n
	return f
}

// T-2 / AC-1
func TestRegistry(t *testing.T) {
	if _, err := provider.NewRegistry(named("a"), named("a")); err == nil {
		t.Fatal("duplicate name accepted")
	}
	r, err := provider.NewRegistry(named("zeta"), named("alpha"), named("mid"))
	if err != nil {
		t.Fatal(err)
	}
	var names []provider.Name
	for _, p := range r.All() {
		names = append(names, p.Descriptor().Name)
	}
	if len(names) != 3 || names[0] != "alpha" || names[1] != "mid" || names[2] != "zeta" {
		t.Fatalf("All = %v", names)
	}
	if p, err := r.Get("mid"); err != nil || p.Descriptor().Name != "mid" {
		t.Fatalf("Get = %v %v", p, err)
	}
	if _, err := r.Get("nope"); !errors.Is(err, provider.ErrUnknownProvider) {
		t.Fatalf("unknown = %v", err)
	}
	if !r.Has("alpha") || r.Has("nope") {
		t.Fatal("Has")
	}
}

// Every adapter built into the server: credentials are strings, secrets have
// no default, keys are unique and patterns compile.
func TestRegisteredSchemasAreWellFormed(t *testing.T) {
	for _, p := range []provider.Provider{xendit.New(), providertest.New()} {
		name := p.Descriptor().Name
		for _, f := range p.CredentialSchema().Fields {
			if f.Type != provider.TypeString {
				t.Errorf("%s credential %s is %s, want string", name, f.Key, f.Type)
			}
			if f.Secret && f.Default != nil {
				t.Errorf("%s credential %s is secret with a default", name, f.Key)
			}
		}
		for _, s := range []provider.Schema{p.CredentialSchema(), p.SettingsSchema()} {
			seen := map[string]bool{}
			for _, f := range s.Fields {
				if seen[f.Key] {
					t.Errorf("%s: duplicate key %s", name, f.Key)
				}
				seen[f.Key] = true
				if f.Pattern != "" {
					// A value that cannot match must fail, a matching one pass.
					if _, errs := provider.ValidateValues(provider.Schema{Fields: []provider.Field{f}}, map[string]any{f.Key: "\x00"}, "x"); len(errs) == 0 {
						t.Errorf("%s: pattern of %s accepts anything", name, f.Key)
					}
				}
			}
		}
		for _, ch := range p.Capabilities().Channels {
			if !ch.Valid() {
				t.Errorf("%s: unknown channel %s", name, ch)
			}
		}
	}
}

func TestStatusError(t *testing.T) {
	err := provider.NewStatusError(provider.ErrRejected, 401, "unauthorized")
	if !errors.Is(err, provider.ErrRejected) || errors.Is(err, provider.ErrRetryable) {
		t.Fatal("class")
	}
	if code, ok := provider.StatusCode(errors.Join(errors.New("x"), err)); !ok || code != 401 {
		t.Fatalf("status = %d %v", code, ok)
	}
	if _, ok := provider.StatusCode(provider.ErrRejected); ok {
		t.Fatal("a bare class has no status")
	}
}
