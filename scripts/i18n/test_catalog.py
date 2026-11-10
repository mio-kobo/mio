#!/usr/bin/env python3
"""Static tests for the Mio CLI locale catalogs and generated lookup tables."""
from __future__ import annotations

import importlib.util
import json
import re
import sys
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
spec = importlib.util.spec_from_file_location("mio_catalog", HERE / "catalog.py")
catalog = importlib.util.module_from_spec(spec)
spec.loader.exec_module(catalog)

EXPECTED = [
    "ar-SA", "bn-BD", "cs-CZ", "da-DK", "de-DE", "el-GR", "en-GB", "en-US",
    "es-ES", "es-MX", "fa-IR", "fi-FI", "fil-PH", "fr-FR", "he-IL", "hi-IN",
    "hu-HU", "id-ID", "it-IT", "ja-JP", "ko-KR", "ms-MY", "nb-NO", "nl-NL",
    "pl-PL", "pt-BR", "pt-PT", "ro-RO", "ru-RU", "sv-SE", "ta-IN", "te-IN",
    "th-TH", "tr-TR", "uk-UA", "vi-VN", "zh-CN", "zh-HK", "zh-TW",
]


# Static source checks deliberately distinguish text literals from code tokens.
SOURCE_TOKEN = re.compile(
    r'(?P<comment>//[^\n]*|/\*[\s\S]*?\*/)'
    r'|(?P<literal>"(?:\\[\s\S]|[^"\\])*"|`[^`]*`|\'(?:\\.|[^\'\\])*\')'
)
KEY_REFERENCE = re.compile(r'\bi18n\.(?:key_[a-z0-9_]+|Key[A-Z][a-zA-Z0-9]*)\b')
DIRECT_OUTPUT = re.compile(
    r'\b(?:chant|fail|(?:string\.)?concat)\(\s*$'
    r'|\bfmt\.(?:Print(?:ln|f)?\(|Fprint(?:ln|f)?\(\s*(?:out|stdout|stderr|r\.Writer)\s*,)\s*$'
)
# These packages also use string.concat to build terminal output.
CLI_SOURCE_ROOTS = ("cmd/mio", "selfhost/cli", "selfhost/usage", "selfhost/bunkopath",
                    "selfhost/diagnostic", "selfhost/testrunner", "selfhost/lsp/rpc")
# Developer executables have their own stable logs and compiler-driver protocols.
DEVELOPER_ROOTS = {"bootstrap", "embed-bunko", "frontend-driver", "native-driver", "unicodegen"}
TECHNICAL_OUTPUT = {
    ("cmd/mio/main.go", r"  Function %d %s params=%d locals=%d returns=%s:\n"),
    ("selfhost/lsp/rpc/session.mio", "Method not found: "),
    ("selfhost/testrunner/execution.mio", "native test terminated without normal completion; status="),
}


def source_files():
    for base, extension in (("cmd/mio", ".go"), ("internal", ".go"), ("selfhost", ".mio")):
        for path in sorted((ROOT / base).rglob("*" + extension)):
            if path.stem.endswith("_test") or path.name in {
                    "messages_generated.go", "messages_generated.mio", "bunko_data.mio"}:
                continue
            yield path


def scanned_source(path):
    source = path.read_text(encoding="utf-8")
    parts = []
    last = 0
    for match in SOURCE_TOKEN.finditer(source):
        parts.extend((source[last:match.start()], " " * len(match[0])))
        last = match.end()
    parts.append(source[last:])
    return source, "".join(parts)


def direct_prose(path, source, code):
    relative = path.relative_to(ROOT).as_posix()
    cli = any(relative.startswith(root + "/") for root in CLI_SOURCE_ROOTS)
    module = relative.split("/")[1]
    if relative.startswith("selfhost/") and (module in DEVELOPER_ROOTS
                                             or module.startswith("native-aot")
                                             or module.endswith("-compiler")):
        return
    for match in SOURCE_TOKEN.finditer(source):
        if not match["literal"]:
            continue
        value = match[0][1:-1]
        # Disregard common format placeholders, escape sequences, and command syntax.
        words = re.sub(r'%[-+.#0-9]*[a-zA-Z]|\\[nrt"]', " ", value)
        words = re.sub(r'(?:--[a-z][a-z0-9-]*|<[\w .]+>|\bE_[A-Z_]+\b)', " ", words)
        if re.match(r'\s*(?:Usage:|Options:)', value) and cli:
            yield value
        elif not re.search(r'\b[^\W\d_]{2,}\s+[^\W\d_]{2,}\b', words):
            continue
        elif words.lstrip(" :").startswith("mio ") or value.startswith(("{", "[")):
            continue
        elif DIRECT_OUTPUT.search(code[max(0, match.start() - 160):match.start()]):
            if cli or re.search(r'\b(?:chant|fail)\(\s*$', code[max(0, match.start() - 160):match.start()]):
                yield value


class CatalogTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.manifest, cls.locales = catalog.load_manifest()
        cls.catalogs = catalog.load_catalogs(cls.manifest, cls.locales)
        cls.source = cls.catalogs["en-US"]

    def test_required_locale_coverage(self):
        self.assertEqual(sorted(self.catalogs), sorted(EXPECTED))
        self.assertEqual(len(self.catalogs), 39)
        self.assertEqual(self.manifest["default"], "en-US")

    def test_every_locale_has_every_key(self):
        for tag, messages in self.catalogs.items():
            self.assertEqual(set(messages), set(self.source), tag)
            self.assertGreater(len(messages), 90, tag)

    def test_placeholders_are_identical(self):
        for tag, messages in self.catalogs.items():
            for key, value in messages.items():
                self.assertEqual(sorted(catalog.PLACEHOLDER.findall(value)),
                                 sorted(catalog.PLACEHOLDER.findall(self.source[key])), (tag, key))

    def test_no_empty_duplicate_or_control_text(self):
        for tag, messages in self.catalogs.items():
            self.assertEqual(len(messages), len(set(messages)), tag)
            for key, value in messages.items():
                self.assertTrue(value.strip(), (tag, key))
                self.assertEqual(value.strip(), value, (tag, key))
                self.assertNotRegex(value, r"[\x00-\x1f\x7f-\x9f]", (tag, key))
                self.assertNotIn("\ufffd", value, (tag, key))
                self.assertFalse(any(character in value for character in catalog.BIDI_CONTROLS), (tag, key))

    def test_rtl_locales_are_marked(self):
        for tag in ("ar-SA", "he-IL", "fa-IR"):
            self.assertEqual(self.manifest["languages"][0]["direction"] in ("ltr", "rtl"), True)
            self.assertEqual(self.locales[tag]["direction"], "rtl")
        for tag in self.catalogs:
            if tag not in ("ar-SA", "he-IL", "fa-IR"):
                self.assertEqual(self.locales[tag]["direction"], "ltr", tag)

    def test_technical_tokens_are_preserved(self):
        for tag, messages in self.catalogs.items():
            for key, value in messages.items():
                self.assertEqual(set(catalog.OPTION_TOKEN.findall(value)),
                                 set(catalog.OPTION_TOKEN.findall(self.source[key])), (tag, key))
                self.assertEqual(set(catalog.TARGET_TOKEN.findall(value)),
                                 set(catalog.TARGET_TOKEN.findall(self.source[key])), (tag, key))
                for product in catalog.PRODUCTS:
                    if product in self.source[key]:
                        self.assertTrue(product in value or product.replace(" ", "-") in value,
                                        (tag, key, product))

    def test_no_untranslated_stub(self):
        for tag, messages in self.catalogs.items():
            if tag == "en-US":
                continue
            changed = sum(1 for key in self.source if messages[key] != self.source[key])
            if tag == "en-GB":
                self.assertGreater(changed, 0, tag)
            else:
                self.assertGreaterEqual(changed * 2, len(self.source), tag)

    def test_manifest_tags_are_canonical_bcp47(self):
        for tag in self.catalogs:
            self.assertEqual(catalog.normalize_tag(tag), tag)
            self.assertTrue(catalog.TAG.fullmatch(tag), tag)

    def test_generated_files_are_current(self):
        catalog.check_generated()

    def test_generation_is_deterministic(self):
        first = catalog.generated()
        second = catalog.generated()
        self.assertEqual(first, second)

    def test_english_variants_are_distinct(self):
        self.assertNotEqual(self.catalogs["en-US"], self.catalogs["en-GB"])

    def test_source_keys_match_catalog(self):
        expected = {"i18n." + constant(key) for key in self.source
                    for constant in (catalog.mio_key_constant, catalog.key_constant)}
        found = set()
        for path in source_files():
            _, code = scanned_source(path)
            references = set(KEY_REFERENCE.findall(code))
            self.assertFalse(references - expected, (path.relative_to(ROOT), sorted(references - expected)))
            found.update(references)
        unused = sorted(key for key in self.source
                        if "i18n." + catalog.mio_key_constant(key) not in found
                        and "i18n." + catalog.key_constant(key) not in found)
        self.assertEqual(unused, [])

    def test_direct_output_prose_uses_catalog(self):
        found = set()
        for path in source_files():
            source, code = scanned_source(path)
            found.update((path.relative_to(ROOT).as_posix(), text)
                         for text in direct_prose(path, source, code))
        self.assertEqual(sorted(found - TECHNICAL_OUTPUT), [])
        self.assertEqual(sorted(TECHNICAL_OUTPUT - found), [], "remove obsolete exceptions")

    def test_placeholder_validation(self):
        catalog.validate_placeholders("xx", "test.example", "first {1}, then {0}")
        for bad in ("{00}", "{x}", "{0", "0}", "{2}"):
            with self.subTest(bad=bad), self.assertRaises(catalog.CatalogError):
                catalog.validate_placeholders("xx", "test.example", bad)

    def test_platform_tokens_cover_all_native_targets(self):
        for target in ("linux-amd64", "linux-arm64", "darwin-amd64",
                       "darwin-arm64", "windows-amd64", "windows-arm64"):
            self.assertIn(target, catalog.NATIVE_TARGETS)
            self.assertEqual(catalog.TARGET_TOKEN.findall("Build for " + target), [target])
            self.assertEqual(catalog.TARGET_TOKEN.findall(target + "x"), [])

    def test_legacy_fallback_vectors(self):
        vectors = {
            "en-us": "en-US", "zh_cn": "zh-CN", "pt_br": "pt-BR",
            "fr-CA": "fr-FR", "de-AT": "de-DE", "es-AR": "es-ES",
            "zh-HK": "zh-HK", "zh-MO": "zh-HK", "zh-Hant": "zh-TW",
            "zh-Hans": "zh-CN", "pt": "pt-BR", "no": "nb-NO", "iw": "he-IL",
            "in": "id-ID", "tl": "fil-PH", "en-AU": "en-US",
            "ja_JP.UTF-8": "ja-JP", "en_US.UTF-8": "en-US",
            "C": "en-US", "POSIX": "en-US", "unknown": "en-US",
        }
        for value, expected in vectors.items():
            self.assertIn(expected, self.catalogs, value)
            self.assertEqual(catalog.resolve_reference(self.manifest, self.locales, value), expected, value)


if __name__ == "__main__":
    unittest.main(verbosity=2)
