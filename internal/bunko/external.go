package bunko

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"Mio/internal/bunkopm"
)

// ReferenceCompilerVersion tracks the compiler version on this branch (VERSION),
// independently of the language syntax version. A release bump must update both.
const ReferenceCompilerVersion = "2.1.2"

func (l *Loader) configureProject(entry string) error {
	project, e := bunkopm.FindProject(entry)
	if e != nil {
		return e
	}
	if project == "" {
		return nil
	}
	l.external, e = bunkopm.OfflinePackages(project, ReferenceCompilerVersion, bunkopm.Target(runtime.GOOS, runtime.GOARCH))
	if e != nil {
		return e
	}
	m, e := bunkopm.ReadManifest(project)
	if e != nil {
		return e
	}
	l.direct = m.Dependencies
	l.packageDependencies = map[string]map[string]string{}
	if len(l.external) > 0 {
		cache, e := bunkopm.CacheRoot()
		if e != nil {
			return e
		}
		idx, e := bunkopm.OpenCachedIndex(cache)
		if e != nil {
			return e
		}
		for name, dir := range l.external {
			version := filepath.Base(filepath.Dir(dir))
			r, e := idx.Record(name, version)
			if e != nil {
				return e
			}
			l.packageDependencies[name] = r.Dependencies
		}
	}
	return nil
}
func externalName(importPath string) string {
	parts := strings.Split(importPath, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}
func (l *Loader) checkExternal(importPath string, chain []string) error {
	n := externalName(importPath)
	if _, ok := l.external[n]; !ok {
		return nil
	}
	allowed := l.direct
	if len(chain) > 0 {
		parent := externalName(chain[len(chain)-1])
		if deps, ok := l.packageDependencies[parent]; ok {
			if parent == n {
				return nil
			}
			allowed = deps
		}
	}
	if _, ok := allowed[n]; !ok {
		return fmt.Errorf("BUNKO_E_UNDECLARED: %q is not a declared dependency of this package", n)
	}
	return nil
}
