package cloud

import (
	"sort"
	"sync"
)

// Registry maps provider names to implementations.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

func NewRegistry(ps ...Provider) *Registry {
	r := &Registry{providers: map[string]Provider{}}
	for _, p := range ps {
		r.Register(p)
	}
	return r
}

// Register adds or replaces a provider.
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

// Get returns the provider with the given name.
func (r *Registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

// RegionNamer is implemented by providers that know the display names of
// their regions.
type RegionNamer interface {
	RegionName(id string) string
}

// RegionName returns the display name of a provider's region, or "" when
// unknown.
func (r *Registry) RegionName(provider, id string) string {
	if r == nil {
		return ""
	}
	p, ok := r.Get(provider)
	if !ok {
		return ""
	}
	if n, ok := p.(RegionNamer); ok {
		if name := n.RegionName(id); name != id {
			return name
		}
	}
	return ""
}

// Names lists registered provider names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.providers))
	for n := range r.providers {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
