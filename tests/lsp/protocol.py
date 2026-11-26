#!/usr/bin/env python3
"""Exercise the native Mio CLI over framed JSON-RPC."""
import argparse
import json
from pathlib import Path
import queue
import subprocess
import tempfile
import threading
import unittest


ROOT = Path(__file__).resolve().parents[2]
CLI = None


def position(text, offset):
    line = text[:offset].split("\n")
    return {"line": len(line) - 1, "character": len(line[-1].encode("utf-16-le")) // 2}


def span(text, offset, length):
    return {"start": position(text, offset), "end": position(text, offset + length)}


class Session:
    def __init__(self, workspace):
        self.stderr = tempfile.TemporaryFile()
        self.proc = subprocess.Popen(
            [CLI, "lsp", "--stdio", "--bunko-root", ROOT / "bunko"],
            cwd=workspace, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=self.stderr,
        )
        self.output = queue.Queue()
        self.backlog = []
        self.counter = 0
        self.reader = threading.Thread(target=self.read, daemon=True)
        self.reader.start()

    def read(self):
        try:
            with self.proc.stdout as wire:
                for first in wire:
                    headers = {}
                    header = first
                    while header != b"\r\n":
                        name, value = header.split(b":", 1)
                        headers[name.lower()] = value.strip()
                        header = wire.readline()
                    size = int(headers[b"content-length"])
                    body = wire.read(size)
                    if len(body) != size:
                        raise ValueError("Truncated JSON-RPC body")
                    self.output.put(json.loads(body))
        except (OSError, ValueError, KeyError) as exc:
            self.output.put(exc)
        finally:
            self.output.put(EOFError("Native LSP closed stdout"))

    def send(self, method=None, request_id=None, **params):
        message = {"jsonrpc": "2.0"}
        if method:
            message.update(method=method, params=params)
        else:
            message.update(result=None)
        if request_id is not None:
            message["id"] = request_id
        body = json.dumps(message, ensure_ascii=False).encode()
        self.proc.stdin.write(b"Content-Length: %d\r\n\r\n" % len(body) + body)
        self.proc.stdin.flush()

    def receive(self, predicate):
        for i, message in enumerate(self.backlog):
            if predicate(message):
                return self.backlog.pop(i)
        while True:
            message = self.output.get(timeout=60)
            if isinstance(message, Exception):
                raise message
            if message.get("jsonrpc") != "2.0":
                raise ValueError(f"Invalid JSON-RPC envelope {message!r}")
            if predicate(message):
                return message
            self.backlog.append(message)

    def request(self, method, request_id=None, **params):
        if request_id is None:
            self.counter += 1
            request_id = self.counter
        self.send(method, request_id, **params)
        return self.receive(lambda message: message.get("id") == request_id)

    def close(self):
        if self.proc.poll() is None:
            self.proc.kill()
        self.proc.wait(timeout=10)
        self.proc.stdin.close()
        self.reader.join(timeout=10)
        self.stderr.close()


class ProtocolTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory(prefix="mio lsp 日本語 ")
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name).resolve()
        self.wire = Session(self.root)
        self.addCleanup(self.wire.close)

    def result(self, method, **params):
        response = self.wire.request(method, **params)
        self.assertNotIn("error", response)
        return response["result"]

    def error(self, method, code, **params):
        response = self.wire.request(method, **params)
        self.assertNotIn("result", response)
        self.assertEqual(response["error"]["code"], code, response)

    def initialize(self, diagnostics=False, roots=(), **capabilities):
        answer = self.result(
            "initialize", request_id=9007199254740993, rootUri=self.root.as_uri(),
            initializationOptions={"diagnosticsEnabled": diagnostics, "diagnosticsDelayMs": 0},
            capabilities=capabilities,
            workspaceFolders=[{"uri": root.as_uri(), "name": root.name} for root in roots],
        )
        self.wire.send("initialized")
        self.assertEqual(self.wire.receive(lambda msg: msg.get("method") == "window/logMessage")["params"]["type"], 3)
        return answer["capabilities"]

    def file(self, name, contents=None):
        path = self.root / name
        if contents is not None:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(contents.encode())
        return path

    def open(self, file, text, version=1):
        self.wire.send("textDocument/didOpen", textDocument={
            "uri": file.as_uri(), "version": version, "languageId": "mio", "text": text,
        })

    def change(self, file, version, text, **extra):
        self.wire.send("textDocument/didChange", textDocument={"uri": file.as_uri(), "version": version},
                       contentChanges=[{"text": text, **extra}])

    def watch(self, file, kind):
        self.wire.send("workspace/didChangeWatchedFiles", changes=[{"uri": file.as_uri(), "type": kind}])

    def at(self, file, text, offset, **extra):
        return {"textDocument": {"uri": file.as_uri()}, "position": position(text, offset), **extra}

    def test_lifecycle_and_capabilities(self):
        self.error("workspace/symbol", -32002, query="")
        caps = self.initialize(workspace={"didChangeWatchedFiles": {"dynamicRegistration": True}})
        for provider in ("hover", "definition", "references", "documentSymbol", "documentHighlight",
                         "workspaceSymbol", "documentFormatting"):
            self.assertIs(caps[f"{provider}Provider"], True)
        for provider in ("completion", "signatureHelp"):
            self.assertTrue(caps[f"{provider}Provider"]["triggerCharacters"])
        self.assertEqual(caps["renameProvider"], {"prepareProvider": True})
        self.assertEqual(caps["positionEncoding"], "utf-16")
        self.assertEqual(caps["textDocumentSync"]["change"], 2)
        self.assertEqual(set(caps["codeActionProvider"]["codeActionKinds"]), {"quickfix", "source.organizeImports"})
        registration = self.wire.receive(lambda msg: msg.get("method") == "client/registerCapability")
        watcher, = registration["params"]["registrations"]
        self.assertEqual(watcher["method"], "workspace/didChangeWatchedFiles")
        self.wire.send(request_id=registration["id"])
        self.error("workspace/symbol", -32602, query=1)
        self.error("textDocument/rangeFormatting", -32601)
        self.assertIsNone(self.result("shutdown", request_id="終了"))
        self.error("workspace/symbol", -32600, query="")
        self.wire.send("exit")
        self.assertEqual(self.wire.proc.wait(timeout=10), 0)

    def test_highlight_and_navigation(self):
        self.initialize()
        text = ('kaku value = 1;\nwaza use() -> int64 {\n'
                '    chant("😀"); kaku value = 2;\n    value = value + 1;\n'
                '    kaesu value;\n}\nchant(value, use()); // value\n')
        file = self.file("main.mio")
        self.open(file, text)
        local = text.index("value = 2")
        assignment = text.index("value = value")
        read = assignment + len("value = ")
        last = text.index("value;")
        target = self.at(file, text, read + 1)
        highlights = self.result("textDocument/documentHighlight", **target)
        expected = [(local, 3), (assignment, 3), (read, 2), (last, 2)]
        self.assertCountEqual(highlights, [{"range": span(text, at, 5), "kind": kind} for at, kind in expected])
        self.assertEqual(self.result("textDocument/definition", **target),
                         [{"uri": file.as_uri(), "range": span(text, local, 5)}])
        refs = self.result("textDocument/references", **target, context={"includeDeclaration": True})
        self.assertCountEqual([ref["range"] for ref in refs], [span(text, at, 5) for at, _ in expected])
        self.assertIn("int64", json.dumps(self.result("textDocument/hover", **target)))
        self.assertEqual({x["name"] for x in self.result("textDocument/documentSymbol", textDocument={"uri": file.as_uri()})},
                         {"use", "value"})
        use = self.at(file, text, text.rindex("use"))
        self.assertEqual([item["kind"] for item in self.result("textDocument/documentHighlight", **use)], [1, 1])
        self.assertEqual(self.result("textDocument/documentHighlight", **self.at(file, text, text.rindex("value"))), [])

    def test_workspace_search_and_watched_overlays(self):
        self.initialize(roots=(self.root, self.root / "other"))
        first = self.file("first.mio", "waza BuildCounter() -> unit {}\nkata Box { value: int64; }\n")
        self.file("other/second.mio", "teisu counterOther = 2;\nsentaku State { Ready; }\n")
        search = lambda query: self.result("workspace/symbol", query=query)
        self.assertEqual({s["name"] for s in search("")}, {"BuildCounter", "Box", "value", "counterOther", "State", "Ready"})
        match, = search("bCn")
        self.assertEqual(match["kind"], 12)
        self.assertEqual(match["location"]["uri"], first.as_uri())
        decl = "waza BuildCounter() -> unit {}"
        self.assertEqual(match["location"]["range"], span(decl, 0, len(decl)))
        self.assertEqual(match["selectionRange"], span(decl, 5, 12))
        self.assertEqual(search("counter")[0]["name"], "counterOther")
        self.assertEqual(search("ready")[0]["containerName"], "State")

        new = self.file("新/new.mio", "kaku DiskName = 1;\n")
        self.watch(new, 1)
        self.watch(new, 1)
        self.assertEqual([s["name"] for s in search("disk")], ["DiskName"])
        self.open(new, "kaku BufferName = 2;\n")
        self.file("新/new.mio", "kaku DiskChanged = 3;\n")
        self.watch(new, 2)
        self.assertEqual([s["name"] for s in search("buffer")], ["BufferName"])
        self.assertEqual(search("disk"), [])
        self.change(new, 2, "Overlay", range=span("kaku BufferName = 2;\n", 5, 10))
        self.assertEqual([s["name"] for s in search("overlay")], ["Overlay"])
        self.wire.send("textDocument/didClose", textDocument={"uri": new.as_uri()})
        self.assertEqual([s["name"] for s in search("disk")], ["DiskChanged"])
        new.unlink()
        self.watch(new, 3)
        self.assertEqual(search("disk"), [])

    def test_prepare_and_local_rename_capture(self):
        self.initialize()
        text = ('kaku value = 1;\nwaza use() -> int64 {\n'
                '    chant("😀"); kaku value = 2;\n    kaesu value;\n}\n'
                'waza scope() -> int64 { kaku local = 3; kaesu local + value; }\n'
                'chant(value);\n')
        file = self.file("main.mio", text)
        self.open(file, text)
        read = text.index("value;")
        target = self.at(file, text, read + 1)
        self.assertEqual(self.result("textDocument/prepareRename", **target),
                         {"range": span(text, read, 5), "placeholder": "value"})
        self.error("textDocument/prepareRename", -32803, **self.at(file, text, 1))
        self.error("textDocument/prepareRename", -32803, **self.at(file, text, len(text) + 3))
        for invalid in ("waza", "a b", " leading", "", "変数🍡"):
            self.error("textDocument/rename", -32602, **target, newName=invalid)
        self.assertEqual(self.result("textDocument/rename", **target, newName="value"), {"changes": {}})
        outcome = self.result("textDocument/rename", **target, newName="localValue")
        self.assertEqual(outcome["changes"], {file.as_uri(): [
            {"range": span(text, text.index("value = 2"), 5), "newText": "localValue"},
            {"range": span(text, read, 5), "newText": "localValue"},
        ]})
        first = self.at(file, text, text.index("value"))
        self.error("textDocument/rename", -32803, **first, newName="local")
        local = self.at(file, text, text.index("local ="))
        self.error("textDocument/rename", -32803, **local, newName="value")
        self.error("textDocument/rename", -32803, **first, newName="scope")
        self.error("textDocument/rename", -32803, **first, newName="len")
        self.assertEqual(file.read_bytes(), text.encode())

    def test_rename_across_closed_packages_and_same_package(self):
        self.initialize()
        text = "koukai waza value() -> int64 { kaesu 1; }\n"
        declaration = self.file("lib/定義.mio", text)
        sibling_text = "waza use() -> int64 { kaesu value(); }\n"
        sibling = self.file("lib/sibling.mio", sibling_text)
        consumer_text = 'yobu "../lib";\nwaza use() -> int64 { kaesu lib.value(); }\n'
        consumer = self.file("app/main.mio", consumer_text)
        other = self.file("other.mio", "waza value() -> int64 { kaesu 0; }\n")
        self.open(declaration, text)
        target = self.at(declaration, text, text.index("value") + 1)
        outcome = self.result("textDocument/rename", **target, newName="renamed")
        expected = {f.as_uri(): [{"range": span(source, source.index("value"), 5), "newText": "renamed"}]
                    for f, source in [(declaration, text), (sibling, sibling_text), (consumer, consumer_text)]}
        self.assertEqual(outcome["changes"], expected)
        self.assertNotIn(other.as_uri(), outcome["changes"])
        self.assertEqual(self.result("textDocument/rename",
                                     **self.at(consumer, consumer_text, consumer_text.index("value")),
                                     newName="renamed"), outcome)

    def test_rename_type_and_field(self):
        self.initialize()
        text = ("kata Box { value: int64; }\n"
                "waza unpack(b: Box) -> int64 { kaesu b.value; }\n"
                "chant(unpack(Box{value: 3}));\n")
        file = self.file("main.mio", text)
        self.open(file, text)
        for name, replacement, count in [("Box", "Record", 3), ("value", "entry", 3)]:
            with self.subTest(name=name):
                outcome = self.result("textDocument/rename", **self.at(file, text, text.index(name)), newName=replacement)
                edits = outcome["changes"][file.as_uri()]
                self.assertEqual(len(edits), count)
                self.assertTrue(all(edit["newText"] == replacement for edit in edits))

    def test_rename_enum_variant_in_constructor_and_match(self):
        self.initialize()
        text = ("sentaku State { Ready; }\n"
                "waza use(state: State) -> unit { awase (state) { baai Ready { chant(1); } } }\n"
                "use(State.Ready());\n")
        file = self.file("main.mio", text)
        self.open(file, text)
        outcome = self.result("textDocument/rename", **self.at(file, text, text.index("Ready")), newName="Running")
        expected = [span(text, offset, 5) for offset in
                    (text.index("Ready"), text.index("Ready", text.index("baai")), text.rindex("Ready"))]
        self.assertCountEqual(outcome["changes"][file.as_uri()],
                              [{"range": at, "newText": "Running"} for at in expected])

    def test_watched_dependency_refreshes_navigation_and_diagnostics(self):
        self.initialize(diagnostics=True)
        dep = self.file("dep/api.mio", "koukai waza value() -> int64 { kaesu 1; }\n")
        text = 'yobu "./dep";\nwaza use() -> int64 { kaesu dep.value(); }\n'
        main = self.file("main.mio", text)
        self.open(main, text)
        target = self.at(main, text, text.index("value"))
        self.assertEqual(self.result("textDocument/definition", **target)[0]["uri"], dep.as_uri())
        get_diagnostics = lambda: self.wire.receive(
            lambda msg: msg.get("method") == "textDocument/publishDiagnostics")["params"]["diagnostics"]
        self.assertEqual(get_diagnostics(), [])
        self.file("dep/api.mio", "koukai waza changed() -> int64 { kaesu 1; }\n")
        self.watch(dep, 2)
        self.assertTrue(get_diagnostics())
        self.assertIsNone(self.result("textDocument/definition", **target))
        self.file("dep/api.mio", "koukai waza value() -> int64 { kaesu 2; }\n")
        self.watch(dep, 2)
        self.assertEqual(get_diagnostics(), [])
        self.assertEqual(self.result("textDocument/definition", **target)[0]["uri"], dep.as_uri())

    def test_formatting_options_and_idempotency(self):
        self.initialize()
        file = self.file("main.mio")
        source = 'waza f()->unit{\r\n   // note  \r\n   chant("😀  intact",1+2);\r\n}'
        self.open(file, source)
        doc = {"uri": file.as_uri()}
        self.error("textDocument/formatting", -32602, textDocument=doc, options={"tabSize": 0, "insertSpaces": True})
        expected = 'waza f() -> unit {\r\n\t// note  \r\n\tchant("😀  intact", 1 + 2);\r\n}'
        tabs = {"tabSize": 8, "insertSpaces": False}
        edit, = self.result("textDocument/formatting", textDocument=doc, options=tabs)
        self.assertEqual(edit, {"range": span(source, 0, len(source)), "newText": expected})
        self.change(file, 2, expected)
        self.assertEqual(self.result("textDocument/formatting", textDocument=doc, options=tabs), [])
        options = {"tabSize": 2, "insertSpaces": True, "insertFinalNewline": True, "trimFinalNewlines": True}
        edit, = self.result("textDocument/formatting", textDocument=doc, options=options)
        self.assertEqual(edit["newText"], expected.replace("\t", "  ") + "\r\n")
        self.change(file, 3, edit["newText"] + "\r\n\r\n")
        trimmed, = self.result("textDocument/formatting", textDocument=doc, options=options)
        self.assertEqual(trimmed["newText"], edit["newText"])
        self.change(file, 4, "waza f(\n")
        self.error("textDocument/formatting", -32803, textDocument=doc, options=options)

    def test_diagnostics_completion_signature_and_code_actions(self):
        self.initialize(diagnostics=True)
        text = ("waza add(x: int64, y: int64) -> int64 { kaesu x + y; }\n"
                "waza f() -> unit { chant(add(1, 2)); chant(convert.format_int(42)); }\n")
        file = self.file("main.mio", text)
        self.open(file, text)
        diagnostics = self.wire.receive(lambda msg: msg.get("method") == "textDocument/publishDiagnostics")["params"]
        self.assertEqual(diagnostics["uri"], file.as_uri())
        self.assertEqual(diagnostics["version"], 1)
        self.assertTrue(diagnostics["diagnostics"])
        self.assertTrue(all(item.get("range") for item in diagnostics["diagnostics"]))
        signature = self.result("textDocument/signatureHelp", **self.at(file, text, text.index("1, 2") + 3))
        self.assertIn("add(", signature["signatures"][0]["label"])
        self.assertEqual(signature["activeParameter"], 1)
        completion = self.result("textDocument/completion", **self.at(file, text, text.index("format_int") + 3))
        self.assertIn("format_int", [i["label"] for i in completion["items"]])
        actions = self.result("textDocument/codeAction", textDocument={"uri": file.as_uri()},
                              range=span(text, text.index("convert"), len("convert.format_int")),
                              context={"diagnostics": diagnostics["diagnostics"]})
        self.assertEqual({action["kind"] for action in actions}, {"quickfix", "source.organizeImports"})
        for action in actions:
            change, = action["edit"]["documentChanges"]
            self.assertEqual(change["textDocument"], {"uri": file.as_uri(), "version": 1})
            self.assertIn('yobu "kiso/convert";', change["edits"][0]["newText"])
        self.change(file, 2, 'yobu "kiso/convert";\n', range=span(text, 0, 0))
        clean = self.wire.receive(lambda msg: msg.get("method") == "textDocument/publishDiagnostics")["params"]
        self.assertEqual(clean["version"], 2)
        self.assertEqual(clean["diagnostics"], [])
        self.wire.send("textDocument/didClose", textDocument={"uri": file.as_uri()})
        cleared = self.wire.receive(lambda msg: msg.get("method") == "textDocument/publishDiagnostics")["params"]
        self.assertEqual(cleared["diagnostics"], [])


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--native", required=True, type=lambda value: Path(value).resolve())
    args = parser.parse_args()
    CLI = args.native
    unittest.main(argv=[__file__], verbosity=2)
