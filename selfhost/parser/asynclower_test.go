package parser_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// The lowered tree must be a tree: no node id may be reachable through two parents (the resolver binds a node once per visit, and the
// code generator finds closures by node id, so a shared function literal would lose its captures).
func TestAsyncLoweringProducesATree(t *testing.T) {
	root := repositoryRoot(t)
	var paths []string
	for _, pattern := range []string{"tests/async/runtime/*.mio", "tests/async/frontend/*.mio"} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, matches...)
	}
	if extra := os.Getenv("ASYNC_LOWER_EXTRA"); extra != "" {
		paths = append(paths, extra)
	}
	sort.Strings(paths)
	results, _ := runMioParserMode(t, paths, true)
	for index, result := range results {
		if len(result.errors) != 0 || result.root < 0 {
			continue // rejected programs keep their raw tree
		}
		parents := map[int]int{}
		var walk func(id, parent int)
		walk = func(id, parent int) {
			if previous, seen := parents[id]; seen {
				t.Errorf("%s: node %d (%d) is reachable from parents %d and %d", filepath.Base(paths[index]), id, result.nodes[id].kind, previous, parent)
				return
			}
			parents[id] = parent
			for _, child := range result.nodes[id].children {
				walk(child, id)
			}
		}
		walk(result.root, -1)
		t.Logf("%s: %d nodes, %d reachable", filepath.Base(paths[index]), len(result.nodes), len(parents))
	}
}
