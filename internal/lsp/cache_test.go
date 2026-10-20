package lsp

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"Mio/internal/bunko"
)

func syntheticAnalysis(doc *document) *analysis {
	return &analysis{
		URI:        doc.URI,
		Version:    doc.Version,
		Text:       doc.Text,
		Buffer:     newTextBuffer(doc.Text),
		FileText:   map[string]string{},
		FileBuffer: map[string]*textBuffer{},
		Stamps:     map[string]fileStamp{},
	}
}

func TestAnalysisServiceCachesByVersion(t *testing.T) {
	service := newAnalysisService(2, 4)
	doc := &document{URI: "untitled:cache.mio", LanguageID: "mio", Version: 1, Text: "kaku x = 1;"}
	var builds int32
	build := func(context.Context) *analysis {
		atomic.AddInt32(&builds, 1)
		return syntheticAnalysis(doc)
	}
	ctx := context.Background()
	if first := service.get(ctx, doc, build); first == nil {
		t.Fatal("first analysis is nil")
	}
	if second := service.get(ctx, doc, build); second == nil {
		t.Fatal("second analysis is nil")
	}
	if got := atomic.LoadInt32(&builds); got != 1 {
		t.Fatalf("builds = %d, want 1", got)
	}

	updated := &document{URI: doc.URI, LanguageID: "mio", Version: 2, Text: "kaku x = 2;"}
	if result := service.get(ctx, updated, build); result == nil {
		t.Fatal("updated analysis is nil")
	}
	if got := atomic.LoadInt32(&builds); got != 2 {
		t.Fatalf("builds after version change = %d, want 2", got)
	}

	service.invalidate(doc.URI)
	if result := service.get(ctx, updated, build); result == nil {
		t.Fatal("analysis after invalidation is nil")
	}
	if got := atomic.LoadInt32(&builds); got != 3 {
		t.Fatalf("builds after invalidation = %d, want 3", got)
	}
}

func TestAnalysisServiceSingleFlight(t *testing.T) {
	service := newAnalysisService(4, 4)
	doc := &document{URI: "untitled:flight.mio", LanguageID: "mio", Version: 1, Text: "kaku x = 1;"}
	var builds int32
	build := func(context.Context) *analysis {
		atomic.AddInt32(&builds, 1)
		time.Sleep(10 * time.Millisecond)
		return syntheticAnalysis(doc)
	}
	ctx := context.Background()
	const workers = 8
	var wait sync.WaitGroup
	wait.Add(workers)
	results := make([]*analysis, workers)
	for index := 0; index < workers; index++ {
		go func(slot int) {
			defer wait.Done()
			results[slot] = service.get(ctx, doc, build)
		}(index)
	}
	wait.Wait()
	if got := atomic.LoadInt32(&builds); got != 1 {
		t.Fatalf("concurrent builds = %d, want 1", got)
	}
	for index, result := range results {
		if result == nil {
			t.Fatalf("worker %d got nil analysis", index)
		}
	}
}

func TestAnalysisServiceCancellation(t *testing.T) {
	service := newAnalysisService(1, 4)
	doc := &document{URI: "untitled:cancel.mio", LanguageID: "mio", Version: 1, Text: "kaku x = 1;"}
	started := make(chan struct{})
	build := func(ctx context.Context) *analysis {
		close(started)
		<-ctx.Done()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *analysis, 1)
	go func() { done <- service.get(ctx, doc, build) }()
	<-started
	cancel()
	select {
	case result := <-done:
		if result != nil {
			t.Fatalf("cancelled analysis = %#v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled analysis did not return")
	}
}

func TestAnalysisServiceInvalidatesDependencyChanges(t *testing.T) {
	root := t.TempDir()
	libPath := filepath.Join(root, "bunko", "lib", "lib.mio")
	writeMioFile(t, libPath, "koukai waza add(a: int64, b: int64) -> int64 {\n    kaesu a + b;\n}\n")
	mainPath := filepath.Join(root, "main.mio")
	mainText := "yobu \"lib\";\n\nwaza main() -> int64 {\n    kaesu lib.add(1, 2);\n}\n"
	writeMioFile(t, mainPath, mainText)

	instance := newAnalyzer(filepath.Join(root, "bunko"))
	doc := &document{URI: uriFromPath(mainPath), LanguageID: "mio", Version: 1, Text: mainText}
	service := newAnalysisService(2, 4)
	build := func(ctx context.Context) *analysis {
		return instance.analyze(ctx, doc, bunko.NewOverlay(), nil)
	}
	ctx := context.Background()
	first := service.get(ctx, doc, build)
	if first == nil {
		t.Fatal("analysis is nil")
	}
	if len(first.Stamps) == 0 {
		t.Fatal("dependency stamps are empty")
	}
	if builds := service.buildCount(); builds != 1 {
		t.Fatalf("builds = %d, want 1", builds)
	}
	service.get(ctx, doc, build)
	if builds := service.buildCount(); builds != 1 {
		t.Fatalf("cached builds = %d, want 1", builds)
	}

	// 修改依赖文件内容后，缓存必须失效并重新分析。
	writeMioFile(t, libPath, "koukai waza add(a: int64, b: int64) -> int64 {\n    kaesu a + b + 1;\n}\n")
	updated := service.get(ctx, doc, build)
	if updated == nil {
		t.Fatal("updated analysis is nil")
	}
	if builds := service.buildCount(); builds != 2 {
		t.Fatalf("builds after dependency change = %d, want 2", builds)
	}
}

func TestAnalysisServiceEvictsOldEntries(t *testing.T) {
	service := newAnalysisService(2, 2)
	build := func(doc *document) func(context.Context) *analysis {
		return func(context.Context) *analysis { return syntheticAnalysis(doc) }
	}
	docs := []*document{
		{URI: "untitled:a.mio", Version: 1, Text: "kaku a = 1;"},
		{URI: "untitled:b.mio", Version: 1, Text: "kaku b = 1;"},
		{URI: "untitled:c.mio", Version: 1, Text: "kaku c = 1;"},
	}
	for _, doc := range docs {
		if result := service.get(context.Background(), doc, build(doc)); result == nil {
			t.Fatalf("analysis for %s is nil", doc.URI)
		}
	}
	service.mu.Lock()
	entries := len(service.entries)
	service.mu.Unlock()
	if entries > 2 {
		t.Fatalf("entries = %d, want <= 2", entries)
	}
}

func TestAnalysisServiceInvalidateURIPropagatesToDependents(t *testing.T) {
	service := newAnalysisService(2, 4)
	rootDoc := &document{URI: "file:///workspace/main.mio", Version: 1, Text: "yobu \"lib\";"}
	dep := &analysis{
		URI:     rootDoc.URI,
		Version: 1,
		Path:    "/workspace/main.mio",
		Files:   []string{"/workspace/bunko/lib/lib.mio", "/workspace/main.mio"},
		Stamps:  map[string]fileStamp{},
	}
	service.get(context.Background(), rootDoc, func(context.Context) *analysis { return dep })

	libDoc := &document{URI: "file:///workspace/bunko/lib/lib.mio", Version: 1, Text: "koukai waza add() -> int64 { kaesu 1; }"}
	service.get(context.Background(), libDoc, func(context.Context) *analysis { return syntheticAnalysis(libDoc) })

	service.invalidate(libDoc.URI)
	service.mu.Lock()
	_, rootCached := service.entries[rootDoc.URI]
	service.mu.Unlock()
	if rootCached {
		t.Fatal("dependent analysis should be invalidated when a dependency file changes")
	}
}

func generateProgram(functions int) string {
	var builder strings.Builder
	for index := 0; index < functions; index++ {
		fmt.Fprintf(&builder, "waza f%d(a: int64, b: int64) -> int64 {\n", index)
		for slot := 0; slot < 8; slot++ {
			fmt.Fprintf(&builder, "    kaku value%d = a + b + %d;\n", slot, slot)
		}
		fmt.Fprintf(&builder, "    kaesu value7;\n}\n\n")
	}
	return builder.String()
}

func TestSemanticAnalysisLargeProgram(t *testing.T) {
	text := generateProgram(120)
	instance := newAnalyzer("")
	doc := &document{URI: "untitled:large.mio", LanguageID: "mio", Version: 1, Text: text}
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	result := instance.analyze(context.Background(), doc, bunko.NewOverlay(), nil)
	elapsed := time.Since(started)
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	heapDelta := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if result == nil {
		t.Fatal("analysis is nil")
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics[:1])
	}
	if len(result.Index.topLevel[result.DocKey]) != 120 {
		t.Fatalf("top-level symbols = %d", len(result.Index.topLevel[result.DocKey]))
	}
	if elapsed > 10*time.Second {
		t.Fatalf("analysis took %s, want < 10s", elapsed)
	}
	t.Logf(
		"analysis: %d functions, %.1f KiB, took %s, heap delta %+.1f KiB",
		120, float64(len(text))/1024, elapsed.Round(time.Millisecond), float64(heapDelta)/1024,
	)
}

func BenchmarkSemanticAnalysis(b *testing.B) {
	text := generateProgram(80)
	instance := newAnalyzer("")
	doc := &document{URI: "untitled:bench.mio", LanguageID: "mio", Version: 1, Text: text}
	overlay := bunko.NewOverlay()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if result := instance.analyze(context.Background(), doc, overlay, nil); result == nil {
			b.Fatal("analysis is nil")
		}
	}
}
