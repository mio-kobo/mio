#!/usr/bin/env python3
"""Verify editor integration files against the Mio lexer and a real `mio lsp`."""
import json, re, subprocess, sys, tomllib
from pathlib import Path

root = Path(__file__).resolve().parent
repo = root.parent
exe = sys.argv[1] if len(sys.argv) > 1 else "mio"
errors = []

# 1. Keyword lists vs internal/token.
tok = (repo / "internal/token/token.go").read_text()
blocks = re.findall(r"var keywords\w+ = map\[string\]Type\{(.*?)\n\}", tok, re.S)
lexer_kw = {k for b in blocks for k in re.findall(r"\"([a-z]+)\":", b)}
if not lexer_kw:
    errors.append("could not read keywords from internal/token/token.go")
for name, path in [("vim", "vim/syntax/mio.vim"), ("emacs", "emacs/mio-mode.el"),
                   ("sublime", "sublime/Mio.sublime-syntax")]:
    text = (root / path).read_text()
    missing = sorted(k for k in lexer_kw if not re.search(r"\b%s\b" % k, text))
    if missing:
        errors.append(f"{name}: keywords missing: {missing}")

# 2. TOML parses.
try:
    cfg = tomllib.loads((root / "helix/languages.toml").read_text())
    assert cfg["language-server"]["mio"]["args"] == ["lsp", "--stdio"]
except Exception as e:
    errors.append(f"helix/languages.toml: {e}")

# 3. Real server handshake.
def frame(obj):
    b = json.dumps(obj).encode()
    return b"Content-Length: %d\r\n\r\n" % len(b) + b
p = subprocess.Popen([exe, "lsp", "--stdio"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
req = frame({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"processId": None, "rootUri": None, "capabilities": {}}})
req += frame({"jsonrpc": "2.0", "id": 2, "method": "shutdown"}) + frame({"jsonrpc": "2.0", "method": "exit"})
out, _ = p.communicate(req, timeout=30)
body = out.split(b"\r\n\r\n", 1)[1]
n = int(re.search(rb"Content-Length: (\d+)", out).group(1))
caps = json.loads(body[:n])["result"]["capabilities"]
for c in ["hoverProvider", "definitionProvider", "referencesProvider", "documentHighlightProvider",
          "documentSymbolProvider", "workspaceSymbolProvider", "renameProvider",
          "documentFormattingProvider", "completionProvider", "signatureHelpProvider"]:
    if c not in caps:
        errors.append(f"server does not advertise {c}")

if errors:
    print("\n".join("FAIL " + e for e in errors)); sys.exit(1)
print(f"ok: {len(lexer_kw)} keywords, server capabilities present")
