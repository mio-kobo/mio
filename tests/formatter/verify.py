"""Run the same formatter and organizer cases against both Mio CLIs."""

import argparse
import json
from pathlib import Path
import shutil
import subprocess
from tempfile import TemporaryDirectory
import unittest


ROOT = Path(__file__).resolve().parents[2]
CLI = ()


def run(cli, workspace, *args, source=b""):
    return subprocess.run(
        [cli, *args, "--bunko-root", str(workspace / "bunko")], cwd=workspace,
        input=source, capture_output=True, timeout=30, check=False,
    )


def frame(method, request_id=None, **params):
    message = {"jsonrpc": "2.0", "method": method, "params": params}
    if request_id is not None:
        message["id"] = request_id
    data = json.dumps(message, ensure_ascii=False).encode()
    return b"Content-Length: %d\r\n\r\n" % len(data) + data


def replies(stream):
    results = {}
    while stream:
        headers, stream = stream.split(b"\r\n\r\n", 1)
        size = int(headers.split(b":", 1)[1])
        message = json.loads(stream[:size])
        stream = stream[size:]
        if "id" in message:
            results[message["id"]] = message
    return results


class FormatterTests(unittest.TestCase):
    def setUp(self):
        temporary = TemporaryDirectory(prefix="mio fmt ")
        self.addCleanup(temporary.cleanup)
        self.work = Path(temporary.name)
        shutil.copytree(ROOT / "bunko", self.work / "bunko")

    def both(self, source, expected, *options):
        for cli in CLI:
            with self.subTest(cli=cli):
                result = run(cli, self.work, "fmt", "--stdin", *options, source=source)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout, expected)
                again = run(cli, self.work, "fmt", "--stdin", "--check", *options, source=expected)
                self.assertEqual(again.returncode, 0, again.stderr)

    def test_goldens(self):
        cases = [
            (b"\xef\xbb\xbf// heading  \r\n\r\nwaza f()->int64{\r\n\tkaesu -42000; // note  \r\n}",
             b"\xef\xbb\xbf// heading  \r\n\r\nwaza f() -> int64 {\r\n    kaesu -42000; // note  \r\n}\r\n"),
            ("/* 前置 */\nwaza f(\n a:int64,\n b:int64\n)->unit{chant(\"mio  \",a+b);}".encode(),
             "/* 前置 */\nwaza f(\n    a: int64,\n    b: int64\n) -> unit { chant(\"mio  \", a + b); }\n".encode()),
            (b"kata Box{\nvalue:int64;\n}\nsentaku State{\nReady;\n}",
             b"kata Box {\n    value: int64;\n}\nsentaku State {\n    Ready;\n}\n"),
        ]
        for source, expected in cases:
            self.both(source, expected, "--no-organize-imports")

    def test_add_remove_deduplicate_sort(self):
        source = (b'yobu "kiso/string";\nyobu "kiso/convert";\nyobu "kiso/convert";\n\n'
                  b'waza main()->unit{\n kaku now=os.monotonic_ms();\n chant(convert.format_int(now));\n}')
        expected = (b'yobu "kiso/convert";\nyobu "kiso/os";\n\n'
                    b'waza main() -> unit {\n    kaku now = os.monotonic_ms();\n'
                    b'    chant(convert.format_int(now));\n}\n')
        self.both(source, expected)

    def test_shadowing_and_comments(self):
        text = (b'yobu "kiso/os"; // required by the deployment policy\n\n'
                b'kata Value { monotonic_ms: int64; }\n'
                b'waza print(os: Value) -> unit { chant(os.monotonic_ms); }\n')
        self.both(text, text)
        source = text[text.index(b"kata "):]
        self.both(source, source)

    def test_local_export_and_ambiguity(self):
        local = self.work / "support" / "convert"
        local.mkdir(parents=True)
        (local / "convert.mio").write_text(
            "koukai waza format_int(value: int64) -> string { kaesu \"local\"; }\n"
            "koukai waza special() -> int64 { kaesu 1; }\n"
            "waza hidden() -> int64 { kaesu 2; }\n")
        ambiguous = b"waza main() -> unit { chant(convert.format_int(42)); }\n"
        self.both(ambiguous, ambiguous)
        hidden = b"waza main() -> unit { chant(convert.hidden()); }\n"
        self.both(hidden, hidden)
        unique = b"waza main() -> unit { chant(convert.special()); }\n"
        self.both(unique, b'yobu "support/convert";\n\n' + unique)

    def test_recursive_check_diff_and_syntax_safety(self):
        directory = self.work / "sources"
        directory.mkdir()
        original = b"waza f()->int64{kaesu 0;}"
        broken = b"waza broken( {"
        for cli in CLI:
            with self.subTest(cli=cli):
                first, second = directory / "a.mio", directory / "b.mio"
                first.write_bytes(original)
                second.write_bytes(original)
                status = run(cli, self.work, "fmt", "--check", str(directory))
                self.assertEqual(status.returncode, 1, status.stderr)
                diff = run(cli, self.work, "fmt", "--diff", str(directory))
                self.assertEqual(diff.returncode, 0, diff.stderr)
                self.assertIn(b"--- " + str(first).encode(), diff.stdout)
                self.assertIn(b"--- " + str(second).encode(), diff.stdout)
                self.assertEqual(first.read_bytes(), original)
                second.write_bytes(broken)
                status = run(cli, self.work, "fmt", str(directory))
                self.assertEqual(status.returncode, 2, status.stderr)
                self.assertEqual(second.read_bytes(), broken)
                self.assertEqual(first.read_bytes(), b"waza f() -> int64 { kaesu 0; }\n")

    def test_native_lsp(self):
        native = CLI[1]
        uri = (self.work / "main.mio").as_uri()
        doc = {"uri": uri}
        other = {"uri": (self.work / "second.mio").as_uri()}
        text = "waza main() -> unit {\n    os.\n}\n"
        diagnostics = {"diagnostics": []}
        span = {"start": {"line": 1, "character": 4}, "end": {"line": 1, "character": 28}}
        frames = [
            frame("initialize", 1, rootUri=self.work.as_uri()),
            frame("textDocument/didOpen", textDocument={
                **other, "version": 1, "languageId": "mio", "text": "waza other() -> unit {}\n"}),
            frame("textDocument/didOpen", textDocument={
                **doc, "version": 1, "languageId": "mio", "text": text}),
            frame("textDocument/completion", 2, textDocument=doc, position={"line": 1, "character": 7}),
            frame("textDocument/didChange", textDocument={**doc, "version": 2}, contentChanges=[{
                "range": {"start": {"line": 1, "character": 4}, "end": {"line": 1, "character": 7}},
                "text": "chant(convert.format_int(42));"}]),
            frame("textDocument/codeAction", 3, textDocument=doc, range=span, context=diagnostics),
            frame("textDocument/codeAction", 4, textDocument=other, range=span, context=diagnostics),
            frame("textDocument/codeAction", 5, textDocument=doc, range=span, context=diagnostics),
            frame("$/cancelRequest", id=5),
            frame("shutdown", 6),
            frame("exit"),
        ]
        result = run(native, self.work, "lsp", "--stdio", source=b"".join(frames))
        self.assertEqual(result.returncode, 0, result.stderr)
        output = replies(result.stdout)
        items = output[2]["result"]["items"]
        completion = next(item for item in items if item["label"] == "monotonic_ms")
        self.assertIn('yobu "kiso/os";', completion["additionalTextEdits"][0]["newText"])
        self.assertNotIn("runtime", [item["label"] for item in items])
        actions = {action["kind"]: action for action in output[3]["result"]}
        self.assertEqual(set(actions), {"quickfix", "source.organizeImports"})
        for action in actions.values():
            change, = action["edit"]["documentChanges"]
            self.assertEqual(change["textDocument"], {"uri": uri, "version": 2})
            self.assertIn('yobu "kiso/convert";', change["edits"][0]["newText"])
        self.assertEqual(output[4]["result"], [])
        self.assertEqual(output[5]["error"]["code"], -32800)


    def test_native_lsp_reports_ambiguity_and_reads_live_package_exports(self):
        public = "koukai waza value() -> int64 { kaesu 1; }\n"
        for prefix in ("first", "second"):
            package = self.work / prefix / "widget"
            package.mkdir(parents=True)
            (package / "exports.mio").write_text(public)
        edited = (self.work / "first" / "widget" / "exports.mio").as_uri()
        doc = {"uri": (self.work / "main.mio").as_uri()}
        span = {"start": {"line": 1, "character": 10}, "end": {"line": 1, "character": 24}}
        action_context = {"diagnostics": []}
        frames = [
            frame("initialize", 1, rootUri=self.work.as_uri()),
            frame("textDocument/didOpen", textDocument={
                **doc, "version": 1, "languageId": "mio",
                "text": "waza main() -> unit {\n    chant(widget.value());\n}\n"}),
            frame("textDocument/codeAction", 2, textDocument=doc, range=span, context=action_context),
            frame("textDocument/didOpen", textDocument={
                "uri": edited, "version": 1, "languageId": "mio", "text": public}),
            frame("textDocument/didChange", textDocument={"uri": edited, "version": 2},
                  contentChanges=[{"text": public.removeprefix("koukai ")}]),
            frame("textDocument/codeAction", 3, textDocument=doc, range=span, context=action_context),
            frame("shutdown", 4),
            frame("exit"),
        ]
        result = run(CLI[1], self.work, "lsp", "--stdio", source=b"".join(frames))
        self.assertEqual(result.returncode, 0, result.stderr)
        output = replies(result.stdout)
        ambiguous = output[2]["result"]
        self.assertEqual({action["title"] for action in ambiguous},
                         {"Add import: first/widget", "Add import: second/widget"})
        self.assertTrue(all(action["kind"] == "quickfix" for action in ambiguous))
        updated = output[3]["result"]
        self.assertEqual({action["title"] for action in updated},
                         {"Organize imports", "Add import: second/widget"})


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--go", required=True, type=lambda value: Path(value).resolve())
    parser.add_argument("--native", required=True, type=lambda value: Path(value).resolve())
    args = parser.parse_args()
    CLI = (str(args.go), str(args.native))
    unittest.main(argv=[__file__], verbosity=2)
