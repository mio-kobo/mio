package lsp

import (
	"context"
	"encoding/json"

	"Mio/internal/format"
)

type documentFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// requestFormatting implements textDocument/formatting with the same engine as
// `mio fmt`. Formatting options (tabSize, insertSpaces) are ignored: the Mio
// style is fixed. The result is one whole-document edit, or no edits when the
// buffer is already formatted. Buffers that do not parse are refused with an
// explanatory error so the editor never receives a guessed edit.
func (s *server) requestFormatting(_ context.Context, params json.RawMessage) (any, *responseError) {
	var value documentFormattingParams
	if rpcError := decodeRequestParams(params, &value); rpcError != nil {
		return nil, rpcError
	}
	if value.TextDocument.URI == "" {
		return nil, &responseError{Code: codeInvalidParams, Message: "missing textDocument.uri"}
	}
	doc, ok := s.store.snapshot(value.TextDocument.URI)
	if !ok {
		return []TextEdit{}, nil
	}
	formatted, err := format.Format([]byte(doc.Text))
	if err != nil {
		return nil, renameFailure("cannot format: " + err.Error())
	}
	if string(formatted) == doc.Text {
		return []TextEdit{}, nil
	}
	buffer := newTextBuffer(doc.Text)
	return []TextEdit{{
		Range:   Range{Start: Position{}, End: buffer.positionAt(len(doc.Text))},
		NewText: string(formatted),
	}}, nil
}
