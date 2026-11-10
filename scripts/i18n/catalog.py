#!/usr/bin/env python3
"""Validate Mio CLI locale catalogs and generate the Go and Mio lookup tables.

The JSON catalogs under selfhost/i18n/locales are the single source of truth.
This script has no third-party dependencies and is used by CI and by
scripts/i18n/test_catalog.py.
"""
from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
I18N = ROOT / "selfhost" / "i18n"
LOCALES = I18N / "locales"
MANIFEST = I18N / "languages.json"
MIO_OUTPUT = I18N / "messages_generated.mio"
GO_OUTPUT = ROOT / "internal" / "i18n" / "messages_generated.go"

PLACEHOLDER = re.compile(r"\{([0-9]+)\}")
KEY = re.compile(r"^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9_]*)+$")
TAG = re.compile(r"^[A-Za-z]{2,3}(?:-[A-Za-z]{4})?(?:-[A-Za-z]{2}|-[0-9]{3})?$")
C0_OR_C1 = re.compile(r"[\x00-\x1f\x7f-\x9f]")
BIDI_CONTROLS = set("\u200e\u200f\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069")
OPTION_TOKEN = re.compile(r"--[a-z][a-z0-9-]*")
TARGET_TOKEN = re.compile(r"(?:darwin-arm64|linux-amd64|windows-amd64)")
PRODUCTS = ("Mio", "Bunko", "Mochi", "Native AOT", "BCP 47")


class CatalogError(ValueError):
    """A locale catalog or generated artifact is inconsistent."""


def fail(message):
    raise CatalogError(message)


def no_duplicates(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            fail("duplicate JSON key %r" % key)
        result[key] = value
    return result


def read_json(path):
    try:
        text = path.read_text(encoding="utf-8")
    except UnicodeDecodeError as error:
        fail("%s is not valid UTF-8: %s" % (path, error))
    if text.startswith("\ufeff"):
        fail("%s starts with a byte-order mark" % path)
    try:
        return json.loads(text, object_pairs_hook=no_duplicates)
    except json.JSONDecodeError as error:
        fail("%s is not valid JSON: %s" % (path, error))


def normalize_tag(value):
    """Canonical BCP 47 case for language, script and region subtags.

    This mirrors the runtime normalization in internal/i18n and selfhost/i18n.
    """
    if not isinstance(value, str):
        return ""
    text = value.strip()
    for separator in (".", "@"):
        cut = text.find(separator)
        if cut >= 0:
            text = text[:cut]
    text = text.replace("_", "-")
    parts = [part for part in text.split("-") if part]
    if not parts:
        return ""
    language = parts[0].lower()
    if language in ("c", "posix"):
        return ""
    if not re.fullmatch(r"[a-z]{2,3}|[a-z]{5,8}", language):
        return ""
    normalized = [language]
    for part in parts[1:]:
        if re.fullmatch(r"[A-Za-z]{4}", part):
            normalized.append(part.title())
        elif re.fullmatch(r"[A-Za-z]{2}", part) or re.fullmatch(r"[0-9]{3}", part):
            normalized.append(part.upper())
        else:
            normalized.append(part.lower())
    return "-".join(normalized)


def load_manifest():
    manifest = read_json(MANIFEST)
    if manifest.get("schema") != 1:
        fail("languages.json schema must be 1")
    languages = manifest.get("languages")
    if not isinstance(languages, list) or not languages:
        fail("languages.json must define languages")
    seen_languages = set()
    seen_locales = set()
    locales = {}
    for entry in languages:
        if not isinstance(entry, dict):
            fail("each language entry must be an object")
        language = entry.get("language")
        primary = entry.get("primary")
        direction = entry.get("direction")
        group = entry.get("locales")
        if not isinstance(language, str) or not re.fullmatch(r"[a-z]{2,3}", language):
            fail("invalid language code %r" % (language,))
        if language in seen_languages:
            fail("duplicate language %r" % language)
        seen_languages.add(language)
        if direction not in ("ltr", "rtl"):
            fail("invalid direction for %s" % language)
        if not isinstance(group, list) or not group:
            fail("language %s has no locales" % language)
        for tag in group:
            canonical = normalize_tag(tag)
            if canonical != tag or not TAG.fullmatch(tag):
                fail("locale tag %r is not canonical BCP 47" % (tag,))
            if tag in seen_locales:
                fail("duplicate locale %r" % tag)
            seen_locales.add(tag)
            parts = tag.split("-")
            if parts[0] != language:
                fail("locale %s does not belong to language %s" % (tag, language))
            locales[tag] = {"language": language, "direction": direction}
        if primary not in group:
            fail("primary locale %r of %s is not in its locale list" % (primary, language))
        locales[primary]["primary"] = True
    default = normalize_tag(manifest.get("default", ""))
    if default not in locales:
        fail("default locale %r is not supported" % default)
    for field in ("aliases", "regional", "scripts"):
        table = manifest.get(field, {})
        if not isinstance(table, dict):
            fail("%s must be an object" % field)
        for key, value in table.items():
            if not isinstance(key, str) or not isinstance(value, str):
                fail("%s must map strings to strings" % field)
            if field == "aliases":
                if not re.fullmatch(r"[a-z]{2,3}", key):
                    fail("alias source %r is not a language code" % key)
                if value not in seen_languages:
                    fail("alias target %r is not a supported language" % value)
            else:
                canonical_key = normalize_tag(key)
                if not canonical_key or canonical_key.split("-")[0] not in seen_languages:
                    fail("%s key %r does not name a supported language" % (field, key))
                if normalize_tag(value) not in locales:
                    fail("%s target %r is not a supported locale" % (field, value))
    return manifest, locales


def load_catalogs(manifest, locales):
    files = {path.stem: path for path in LOCALES.glob("*.json")}
    if set(files) != set(locales):
        missing = sorted(set(locales) - set(files))
        extra = sorted(set(files) - set(locales))
        fail("locale files do not match languages.json; missing=%s extra=%s" % (missing, extra))
    catalogs = {}
    for tag in sorted(locales):
        data = read_json(files[tag])
        if data.get("schema") != 1:
            fail("%s schema must be 1" % tag)
        if data.get("locale") != tag:
            fail("%s declares locale %r" % (tag, data.get("locale")))
        if data.get("direction") != locales[tag]["direction"]:
            fail("%s declares direction %r, manifest says %r" %
                 (tag, data.get("direction"), locales[tag]["direction"]))
        messages = data.get("messages")
        if not isinstance(messages, dict) or not messages:
            fail("%s has no messages" % tag)
        for key, value in messages.items():
            if not isinstance(key, str) or not KEY.fullmatch(key):
                fail("%s has invalid message key %r" % (tag, key))
            if not isinstance(value, str):
                fail("%s.%s is not a string" % (tag, key))
            if value == "" or value.strip() != value:
                fail("%s.%s is empty or has surrounding whitespace" % (tag, key))
            if C0_OR_C1.search(value):
                fail("%s.%s contains a control character or ANSI escape" % (tag, key))
            if "\ufffd" in value or any(control in value for control in BIDI_CONTROLS):
                fail("%s.%s contains a replacement or bidi control character" % (tag, key))
            indexes = [int(index) for index in PLACEHOLDER.findall(value)]
            if indexes and indexes != list(range(len(indexes))):
                fail("%s.%s has non-contiguous placeholders" % (tag, key))
        catalogs[tag] = messages
    source = catalogs[manifest["default"]]
    for tag, messages in catalogs.items():
        if set(messages) != set(source):
            missing = sorted(set(source) - set(messages))
            extra = sorted(set(messages) - set(source))
            fail("%s keys differ from %s; missing=%s extra=%s" %
                 (tag, manifest["default"], missing, extra))
    for tag, messages in catalogs.items():
        for key, value in messages.items():
            expected = sorted(PLACEHOLDER.findall(source[key]))
            actual = sorted(PLACEHOLDER.findall(value))
            if expected != actual:
                fail("%s.%s placeholders %s differ from %s" % (tag, key, actual, expected))
            ours = set(OPTION_TOKEN.findall(value))
            theirs = set(OPTION_TOKEN.findall(source[key]))
            if ours != theirs:
                fail("%s.%s changes option tokens %s -> %s" % (tag, key, sorted(theirs), sorted(ours)))
            if set(TARGET_TOKEN.findall(value)) != set(TARGET_TOKEN.findall(source[key])):
                fail("%s.%s changes platform target tokens" % (tag, key))
            for product in PRODUCTS:
                if product in source[key] and product not in value and product.replace(" ", "-") not in value:
                    fail("%s.%s drops the untranslated name %r" % (tag, key, product))
    for tag, messages in catalogs.items():
        if tag == manifest["default"]:
            continue
        changed = sum(1 for key in source if messages[key] != source[key])
        if tag == "en-GB":
            if changed == 0:
                fail("en-GB is identical to en-US; it must use British spelling")
        elif changed * 2 < len(source):
            fail("%s translates fewer than half of the messages (%d/%d)" %
                 (tag, changed, len(source)))
    return catalogs


def resolve_reference(manifest, locales, value):
    """Reference implementation of the documented BCP 47 fallback chain."""
    tag = normalize_tag(value)
    if tag == "":
        return manifest["default"]
    if tag in locales:
        return tag
    language = tag.split("-")[0]
    region = tag.split("-")[-1]
    regional = manifest.get("regional", {})
    if region != language:
        candidate = language + "-" + region
        if candidate in locales:
            return candidate
        if candidate in regional and regional[candidate] in locales:
            return regional[candidate]
    parts = tag.split("-")
    if len(parts) > 1 and len(parts[1]) == 4:
        script = parts[0] + "-" + parts[1]
        if script in manifest.get("scripts", {}):
            return manifest["scripts"][script]
    language = manifest.get("aliases", {}).get(language, language)
    for entry in manifest["languages"]:
        if entry["language"] == language:
            return entry["primary"]
    return manifest["default"]


def key_constant(key):
    parts = re.split(r"[._-]", key)
    return "Key" + "".join(part[:1].upper() + part[1:] for part in parts)


def mio_key_constant(key):
    return "key_" + key.replace(".", "_").replace("-", "_")


def mio_function(tag):
    return "text_" + tag.lower().replace("-", "_")


def mio_quote(value):
    return '"' + (value.replace("\\", "\\\\").replace('"', '\\"')
                  .replace("\n", "\\n").replace("\r", "\\r").replace("\t", "\\t")) + '"'


def go_quote(value):
    out = ['"']
    for character in value:
        code = ord(character)
        if character == "\\":
            out.append("\\\\")
        elif character == '"':
            out.append('\\"')
        elif character == "\n":
            out.append("\\n")
        elif character == "\r":
            out.append("\\r")
        elif character == "\t":
            out.append("\\t")
        elif code < 0x20:
            out.append("\\u%04x" % code)
        else:
            out.append(character)
    out.append('"')
    return "".join(out)


def ordered_keys(catalogs, default):
    return sorted(catalogs[default])


def render_mio(manifest, locales, catalogs):
    keys = ordered_keys(catalogs, manifest["default"])
    lines = [
        "// Generated by scripts/i18n/catalog.py from locales/*.json; do not edit.",
        "// One escaped table per locale keeps native compilation small; lookup is a",
        "// linear search over keys, and values are UTF-8 text with {0} placeholders.",
        'yobu "kiso/string";',
        'yobu "kiso/array";',
    ]
    for key in keys:
        lines.append("koukai teisu %s = %s;" % (mio_key_constant(key), mio_quote(key)))
    lines.append("")
    lines.append("koukai waza supported_tag(tag: string) -> bool {")
    for tag in sorted(catalogs):
        lines.append('    moshi (tag == %s) { kaesu hai; }' % mio_quote(tag))
    lines.append("    kaesu iie;")
    lines.append("}")
    lines.append("")
    lines.append("koukai waza supported_tags() -> []string {")
    lines.append("    kaesu [" + ", ".join(mio_quote(tag) for tag in sorted(catalogs)) + "];")
    lines.append("}")
    lines.append("")
    lines.append("koukai waza direction(tag: string) -> string {")
    for tag in sorted(catalogs):
        lines.append('    moshi (tag == %s) { kaesu %s; }' % (mio_quote(tag), mio_quote(locales[tag]["direction"])))
    lines.append('    kaesu "ltr";')
    lines.append("}")
    lines.append("")
    lines.append("koukai waza primary(language: string) -> string {")
    for entry in manifest["languages"]:
        lines.append('    moshi (language == %s) { kaesu %s; }' %
                     (mio_quote(entry["language"]), mio_quote(entry["primary"])))
    lines.append('    kaesu %s;' % mio_quote(manifest["default"]))
    lines.append("}")
    lines.append("")
    lines.append("koukai waza alias(language: string) -> string {")
    for source, target in manifest.get("aliases", {}).items():
        lines.append('    moshi (language == %s) { kaesu %s; }' % (mio_quote(source), mio_quote(target)))
    lines.append("    kaesu language;")
    lines.append("}")
    lines.append("")
    lines.append("koukai waza regional(tag: string) -> string {")
    for source, target in manifest.get("regional", {}).items():
        lines.append('    moshi (tag == %s) { kaesu %s; }' % (mio_quote(source), mio_quote(target)))
    lines.append("    kaesu tag;")
    lines.append("}")
    lines.append("")
    lines.append("koukai waza script_default(tag: string) -> string {")
    for source, target in manifest.get("scripts", {}).items():
        lines.append('    moshi (tag == %s) { kaesu %s; }' % (mio_quote(source), mio_quote(target)))
    lines.append('    kaesu "";')
    lines.append("}")
    lines.append("")
    lines.append("waza table_slice(data: []uint8, first: int64, end: int64) -> string {")
    lines.append("    kaku bytes: []uint8 = []; kaku at = first;")
    lines.append("    aida (at < end) { array.push(bytes, data[at]); at = at + 1; }")
    lines.append('    kaku value = string.from_utf8(bytes); moshi (value == mu) { kaesu ""; } kaesu value;')
    lines.append("}")
    lines.append("")
    lines.append("waza table_lookup(table: string, key: string) -> string {")
    lines.append('    kaku needle = string.concat("\\n", string.concat(key, "\\t"));')
    lines.append("    kaku position = string.index(table, needle);")
    lines.append('    moshi (position < 0) { kaesu ""; }')
    lines.append("    kaku data = string.to_bytes(table);")
    lines.append("    kaku start = position + len(string.to_bytes(needle)); kaku end = start;")
    lines.append("    aida (end < len(data) && data[end] != uint8(10)) { end = end + 1; }")
    lines.append("    kaesu table_slice(data, start, end);")
    lines.append("}")
    for tag in sorted(catalogs):
        entries = [key + "\t" + catalogs[tag][key] for key in keys]
        chunks = [entries[index:index + 20] for index in range(0, len(entries), 20)]
        lines.append("")
        lines.append("waza %s() -> string {" % mio_function(tag))
        lines.append("    kaku output = string.new_builder();")
        for chunk in chunks:
            lines.append("    string.write_string(output, %s);" % mio_quote("\n" + "\n".join(chunk)))
        lines.append("    kaesu string.builder_string(output);")
        lines.append("}")
    lines.append("")
    lines.append("waza localized(language: string, key: string) -> string {")
    for tag in sorted(catalogs):
        lines.append("    moshi (language == %s) { kaesu table_lookup(%s(), key); }" % (mio_quote(tag), mio_function(tag)))
    lines.append('    kaesu "";')
    lines.append("}")
    lines.append("")
    lines.append("// Missing entries fall back to en-US and finally to the key itself.")
    lines.append("koukai waza text(language: string, key: string) -> string {")
    lines.append("    kaku value = localized(language, key);")
    lines.append('    moshi (value != "") { kaesu value; }')
    lines.append("    kaku fallback = table_lookup(%s(), key);" % mio_function(manifest["default"]))
    lines.append('    moshi (fallback != "") { kaesu fallback; }')
    lines.append("    kaesu key;")
    lines.append("}")
    return "\n".join(lines) + "\n"


def render_go(manifest, locales, catalogs):
    keys = ordered_keys(catalogs, manifest["default"])
    lines = [
        "// Code generated by scripts/i18n/catalog.py from selfhost/i18n/locales/*.json; DO NOT EDIT.",
        "",
        "package i18n",
        "",
        "// Message identifiers shared with selfhost/i18n/messages_generated.mio.",
    ]
    for key in keys:
        lines.append("const %s Key = %s" % (key_constant(key), go_quote(key)))
    lines.append("")
    lines.append("var messages = map[Language]map[Key]string{")
    width = max(len(key_constant(key)) for key in keys)
    for tag in sorted(catalogs):
        lines.append("\tLanguage(%s): {" % go_quote(tag))
        for key in keys:
            lines.append("\t\t%s:%s %s," % (key_constant(key), " " * (width - len(key_constant(key))), go_quote(catalogs[tag][key])))
        lines.append("\t},")
    lines.append("}")
    lines.append("")
    lines.append("// supported returns every locale with a catalog, ordered deterministically.")
    lines.append("func supported() []Language {")
    lines.append("\treturn []Language{")
    for tag in sorted(catalogs):
        lines.append("\t\tLanguage(%s)," % go_quote(tag))
    lines.append("\t}")
    lines.append("}")
    lines.append("")
    def mapped(name, pairs, value):
        widths = max(len(key) for key, _ in pairs)
        lines.append("var %s = map[Language]string{" % name if name == "directions" else "var %s = map[string]%s{" % (name, value))
        for key, item in pairs:
            lines.append("\t%s:%s %s," % (key, " " * (widths - len(key)), item))
        lines.append("}")
        lines.append("")
    mapped("directions", [('Language(%s)' % go_quote(tag), go_quote(locales[tag]["direction"])) for tag in sorted(catalogs)], "string")
    mapped("primaries", [(go_quote(entry["language"]), "Language(%s)" % go_quote(entry["primary"])) for entry in manifest["languages"]], "Language")
    mapped("aliases", [(go_quote(source), go_quote(target)) for source, target in manifest.get("aliases", {}).items()], "string")
    mapped("regionalTags", [(go_quote(source), "Language(%s)" % go_quote(target)) for source, target in manifest.get("regional", {}).items()], "Language")
    mapped("scriptDefaults", [(go_quote(source), "Language(%s)" % go_quote(target)) for source, target in manifest.get("scripts", {}).items()], "Language")
    lines.append("const defaultLanguage = Language(%s)" % go_quote(manifest["default"]))
    return "\n".join(lines) + "\n"


def generated():
    manifest, locales = load_manifest()
    catalogs = load_catalogs(manifest, locales)
    return {MIO_OUTPUT: render_mio(manifest, locales, catalogs),
            GO_OUTPUT: render_go(manifest, locales, catalogs)}


def check_generated():
    for path, expected in generated().items():
        try:
            actual = path.read_text(encoding="utf-8")
        except OSError:
            fail("missing generated file %s; run scripts/i18n/catalog.py generate" % path)
        if actual != expected:
            fail("%s is stale; run scripts/i18n/catalog.py generate" % path)


def write_generated():
    for path, text in generated().items():
        path.parent.mkdir(parents=True, exist_ok=True)
        with path.open("w", encoding="utf-8", newline="\n") as stream:
            stream.write(text)
        print("wrote %s" % path.relative_to(ROOT))


def main(argv):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("check", "generate"))
    parser.add_argument("--check-generated", action="store_true",
                        help="also verify that the committed generated files are current")
    args = parser.parse_args(argv)
    try:
        if args.command == "generate":
            write_generated()
        manifest, locales = load_manifest()
        catalogs = load_catalogs(manifest, locales)
        if args.check_generated:
            check_generated()
    except CatalogError as error:
        print("i18n catalog: %s" % error, file=sys.stderr)
        return 1
    print("i18n catalog: %d locales, %d messages" %
          (len(catalogs), len(catalogs[manifest["default"]])))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
