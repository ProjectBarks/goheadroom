package parity

import "sort"

type Registry struct {
	comparators map[string]Comparator
}

func NewRegistry() *Registry {
	return &Registry{comparators: make(map[string]Comparator)}
}

func (r *Registry) Register(c Comparator)               { r.comparators[c.Name()] = c }
func (r *Registry) Get(name string) (Comparator, bool)   { c, ok := r.comparators[name]; return c, ok }

func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.comparators))
	for n := range r.comparators {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (r *Registry) All() []Comparator {
	names := r.Names()
	out := make([]Comparator, len(names))
	for i, n := range names {
		out[i] = r.comparators[n]
	}
	return out
}
