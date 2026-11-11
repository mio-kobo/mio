package bunkopm

import "strings"

// Index is a complete, revision-consistent view of official metadata and policy.
// Its interface also lets tests run without network access.
type Index interface {
	Record(name, version string) (Record, error)
	Names() []string
	Versions(name string) []string
}
type Client struct {
	Index            Index
	Cache            string
	Compiler, Target string
	Fetch            *Fetcher
}

func (c *Client) Resolve(roots map[string]string) ([]Record, error) {
	if e := validateDependencies(roots); e != nil {
		return nil, e
	}
	selected := map[string]string{}
	paths := map[string]string{}
	active := map[string]bool{}
	done := map[string]bool{}
	var result []Record
	var visit func(string, string, []string) error
	visit = func(n, v string, chain []string) error {
		label := n + "@" + v
		chain = append(append([]string{}, chain...), label)
		trace := strings.Join(chain, " -> ")
		if len(chain) > 256 {
			return fail("BUNKO_E_GRAPH_LIMIT", "dependency graph exceeds limits")
		}
		if active[n] {
			return fail("BUNKO_E_CYCLE", "%s", trace)
		}
		if old, ok := selected[n]; ok && old != v {
			return fail("BUNKO_E_VERSION_CONFLICT", "%s; %s", paths[n], trace)
		}
		if done[n] {
			return nil
		}
		if _, ok := selected[n]; !ok && len(selected) >= 2000 {
			return fail("BUNKO_E_GRAPH_LIMIT", "dependency graph exceeds 2000 packages")
		}
		selected[n] = v
		paths[n] = trace
		active[n] = true
		r, e := c.Index.Record(n, v)
		if e != nil {
			return e
		}
		if e = r.Compatible(c.Compiler, c.Target); e != nil {
			return e
		}
		for _, dep := range Sorted(r.Dependencies) {
			if e = visit(dep, r.Dependencies[dep], chain); e != nil {
				return e
			}
		}
		active[n] = false
		done[n] = true
		result = append(result, r)
		return nil
	}
	for _, n := range Sorted(roots) {
		if e := visit(n, roots[n], nil); e != nil {
			return nil, e
		}
	}
	return result, nil
}
func (c *Client) Install(dir string) error {
	m, e := ReadManifest(dir)
	if e != nil {
		return e
	}
	r, e := c.Resolve(m.Dependencies)
	if e != nil {
		return e
	}
	return c.InstallRecords(r)
}
func (c *Client) InstallRecords(records []Record) error {
	for _, r := range records {
		if e := c.install(r); e != nil {
			return e
		}
	}
	return nil
}
