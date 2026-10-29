package provider

import (
	"fmt"
	"slices"
	"strings"
)

// Registry holds the provider adapters built into the server. It is filled
// once at startup and only read afterwards.
type Registry struct {
	byName map[Name]Provider
}

// NewRegistry registers ps. Two adapters with one name, or an adapter that
// declares ListTransactions without implementing TransactionLister, is an
// error.
func NewRegistry(ps ...Provider) (*Registry, error) {
	r := &Registry{byName: make(map[Name]Provider, len(ps))}
	for _, p := range ps {
		n := p.Descriptor().Name
		if _, dup := r.byName[n]; dup {
			return nil, fmt.Errorf("provider %q registered twice", n)
		}
		if _, lists := p.(TransactionLister); p.Capabilities().ListTransactions && !lists {
			return nil, fmt.Errorf("provider %q declares list_transactions but cannot list transactions", n)
		}
		r.byName[n] = p
	}
	return r, nil
}

// Get returns the adapter registered under n.
func (r *Registry) Get(n Name) (Provider, error) {
	if p, ok := r.byName[n]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, n)
}

// Has reports whether an adapter is registered under n.
func (r *Registry) Has(n Name) bool {
	_, ok := r.byName[n]
	return ok
}

// All returns every adapter sorted by name.
func (r *Registry) All() []Provider {
	out := make([]Provider, 0, len(r.byName))
	for _, p := range r.byName {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b Provider) int {
		return strings.Compare(string(a.Descriptor().Name), string(b.Descriptor().Name))
	})
	return out
}
