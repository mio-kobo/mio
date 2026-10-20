package lsp

import (
	"fmt"
	"sync"
)

// document 保存一份已打开文档的内存状态
type document struct {
	URI        string
	LanguageID string
	Version    int
	Text       string
}

func (d *document) buffer() *textBuffer {
	return newTextBuffer(d.Text)
}

// documentStore 串行维护打开文档及其版本
type documentStore struct {
	mu   sync.RWMutex
	docs map[string]*document
}

func newDocumentStore() *documentStore {
	return &documentStore{docs: make(map[string]*document)}
}

// open 记录 didOpen 文档；重复打开会覆盖旧状态
func (s *documentStore) open(item TextDocumentItem) *document {
	doc := &document{
		URI:        item.URI,
		LanguageID: item.LanguageID,
		Version:    item.Version,
		Text:       item.Text,
	}
	s.mu.Lock()
	s.docs[item.URI] = doc
	s.mu.Unlock()
	return snapshotDocument(doc)
}

// change 应用增量或全文变更。
// version 不大于当前版本时返回 stale，防止过期编辑回滚缓冲区。
func (s *documentStore) change(
	uri string,
	version int,
	changes []TextDocumentContentChangeEvent,
) (doc *document, stale bool, warnings []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, ok := s.docs[uri]
	if !ok {
		return nil, false, []string{fmt.Sprintf("didChange for unopened document %s", uri)}
	}
	if version <= current.Version {
		return nil, true, []string{fmt.Sprintf(
			"ignored out-of-order didChange for %s: version %d <= %d",
			uri, version, current.Version,
		)}
	}
	text, warnings := applyContentChanges(current.Text, changes)
	updated := &document{
		URI:        current.URI,
		LanguageID: current.LanguageID,
		Version:    version,
		Text:       text,
	}
	s.docs[uri] = updated
	return snapshotDocument(updated), false, warnings
}

// replaceText 用客户端在 didSave 中附带的内容更新缓冲区，保持版本号不变
func (s *documentStore) replaceText(uri, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.docs[uri]
	if !ok {
		return
	}
	updated := *current
	updated.Text = text
	s.docs[uri] = &updated
}

// close 删除文档并返回关闭前的快照
func (s *documentStore) close(uri string) *document {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.docs[uri]
	if !ok {
		return nil
	}
	delete(s.docs, uri)
	return snapshotDocument(current)
}

// snapshot 返回文档副本
func (s *documentStore) snapshot(uri string) (*document, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	current, ok := s.docs[uri]
	if !ok {
		return nil, false
	}
	return snapshotDocument(current), true
}

// all 返回全部打开文档的副本
func (s *documentStore) all() []*document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*document, 0, len(s.docs))
	for _, current := range s.docs {
		result = append(result, snapshotDocument(current))
	}
	return result
}

func snapshotDocument(doc *document) *document {
	copy := *doc
	return &copy
}

// applyContentChanges 按顺序应用内容变更。
// 无 Range 的变更表示全文替换；非法 Range 会被夹取并记录 warning。
func applyContentChanges(text string, changes []TextDocumentContentChangeEvent) (string, []string) {
	warnings := make([]string, 0)
	current := text
	for index, change := range changes {
		if change.Range == nil {
			current = change.Text
			continue
		}
		buffer := newTextBuffer(current)
		start := buffer.offsetAt(change.Range.Start)
		end := buffer.offsetAt(change.Range.End)
		if start > end {
			warnings = append(warnings, fmt.Sprintf(
				"content change %d has inverted range, swapping start and end", index,
			))
			start, end = end, start
		}
		if start < 0 {
			start = 0
		}
		if end > len(current) {
			end = len(current)
		}
		if start > len(current) {
			start = len(current)
		}
		if start > end {
			end = start
		}
		current = current[:start] + change.Text + current[end:]
	}
	return current, warnings
}
