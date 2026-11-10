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

    def test_all_keys_are_referenced_by_a_cli_source(self):
        texts = []
        for path in [ROOT / "selfhost" / "cli" / "main.mio",
                     ROOT / "selfhost" / "usage" / "usage.mio",
                     ROOT / "selfhost" / "testrunner" / "command.mio",
                     ROOT / "selfhost" / "testrunner" / "report.mio",
                     ROOT / "selfhost" / "diagnostic" / "diagnostic.mio",
                     ROOT / "selfhost" / "lsp" / "rpc" / "stdio.mio",
                     ROOT / "cmd" / "mio" / "cli_help.go",
                     ROOT / "cmd" / "mio" / "test_command.go",
                     ROOT / "cmd" / "mio" / "main.go"]:
            texts.append(path.read_text(encoding="utf-8"))
        corpus = "\n".join(texts)
        for key in self.source:
            constant = "key_" + key.replace(".", "_").replace("-", "_")
            go_constant = catalog.key_constant(key)
            self.assertTrue(constant in corpus or go_constant in corpus,
                            "message %r has no CLI reference" % key)

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
