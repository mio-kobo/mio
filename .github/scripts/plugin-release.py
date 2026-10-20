"""Read-only release gates; no credential access or Marketplace upload code."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import re
import urllib.request
import xml.etree.ElementTree as ET
import zipfile

ROOT = Path(__file__).resolve().parents[2]
NUMBER = r"(?:0|[1-9][0-9]*)"
PRE = r"(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)"
SEMVER = rf"{NUMBER}\.{NUMBER}\.{NUMBER}(?:-{PRE}(?:\.{PRE})*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?"


def require(condition, message):
    if not condition:
        raise ValueError(message)


def tag_version(platform, tag):
    match = re.fullmatch(rf"{platform}-V({SEMVER})", tag, flags=re.ASCII)
    require(match is not None, f"Invalid {platform} SemVer tag: {tag!r}")
    return match.group(1)


def check_source(platform, version, gradle_version=None):
    if platform == "jetbrains":
        require(gradle_version == version,
                f"Tag {version} != actual Gradle project.version {gradle_version}")
        return
    package = json.loads((ROOT / "editors/vscode/package.json").read_text())
    lock = json.loads((ROOT / "editors/vscode/package-lock.json").read_text())
    require(package["publisher"] == "exmikupro" and package["name"] == "mio-language-support",
            "VS Code publisher/extension identity changed")
    require(package["version"] == lock["version"] == lock["packages"][""]["version"] == version,
            "Tag/package.json/package-lock.json versions differ")


def check_archive(platform, version, archive):
    with zipfile.ZipFile(archive) as bundle:
        require(bundle.testzip() is None, "Corrupted release archive")
        if platform == "vscode":
            manifest = ET.fromstring(bundle.read("extension.vsixmanifest"))
            identity = manifest.find("{*}Metadata/{*}Identity")
            require(identity is not None, "Missing VSIX Identity")
            require(identity.attrib["Version"] == version and
                    identity.attrib["Publisher"] == "exmikupro" and
                    identity.attrib["Id"] == "mio-language-support", "VSIX Manifest identity/version mismatch")
            package = json.loads(bundle.read("extension/package.json"))
            require(package["version"] == version and package["publisher"] == "exmikupro"
                    and package["name"] == "mio-language-support", "VSIX package.json mismatch")
        else:
            descriptors = []
            for name in bundle.namelist():
                if name.endswith(".jar"):
                    with zipfile.ZipFile(io.BytesIO(bundle.read(name))) as jar:
                        if "META-INF/plugin.xml" in jar.namelist():
                            descriptors.append(ET.fromstring(jar.read("META-INF/plugin.xml")))
            mio = [d for d in descriptors if d.findtext("id") == "com.exmikupro.mio"]
            require(len(mio) == 1 and mio[0].findtext("version") == version,
                    "JetBrains ZIP plugin ID/version mismatch")


def request_json(url, body=None):
    headers = {"Accept": "application/json", "User-Agent": "Mio-release-check"}
    if body is not None:
        headers["Content-Type"] = "application/json"
        headers["Accept"] = "application/json;api-version=7.2-preview.1"
    request = urllib.request.Request(url, data=None if body is None else json.dumps(body).encode(), headers=headers)
    with urllib.request.urlopen(request, timeout=60) as response:
        return json.load(response)


def check_unpublished(platform, version):
    # Network/schema errors fail closed. Include all channels/versions, not just latest.
    if platform == "jetbrains":
        plugin = request_json("https://plugins.jetbrains.com/api/plugins/35197")
        require(plugin["xmlId"] == "com.exmikupro.mio", "Marketplace plugin ID mismatch")
        offset = 0
        while True:
            updates = request_json(f"https://plugins.jetbrains.com/api/plugins/35197/updates?size=100&offset={offset}")
            require(isinstance(updates, list), "Invalid JetBrains update response")
            for update in updates:
                require(update["version"] != version, f"Already published: {platform} {version}")
            if len(updates) < 100:
                break
            offset += len(updates)
    else:
        result = request_json("https://marketplace.visualstudio.com/_apis/public/gallery/extensionquery", {
            "filters": [{"criteria": [{"filterType": 7, "value": "exmikupro.mio-language-support"}]}],
            "flags": 1,
        })
        extensions = [e for r in result["results"] for e in r["extensions"]]
        require(len(extensions) == 1, "Expected existing public VS Code extension")
        extension = extensions[0]
        require(extension["publisher"]["publisherName"] == "exmikupro" and
                extension["extensionName"] == "mio-language-support", "Marketplace extension ID mismatch")
        versions = extension["versions"]
        require(len(versions) > 0, "Marketplace returned no versions")
        require(all(v["version"] != version for v in versions), f"Already published: {platform} {version}")
    print(f"Not yet published: {platform} {version}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("platform", choices=["jetbrains", "vscode"])
    parser.add_argument("tag")
    parser.add_argument("--gradle-version")
    parser.add_argument("--archive", type=Path)
    parser.add_argument("--checksum", action="store_true")
    parser.add_argument("--unpublished", action="store_true")
    args = parser.parse_args()
    version = tag_version(args.platform, args.tag)
    check_source(args.platform, version, args.gradle_version)
    if args.archive:
        check_archive(args.platform, version, args.archive)
        if args.checksum:
            digest = hashlib.sha256(args.archive.read_bytes()).hexdigest()
            args.archive.with_name(args.archive.name + ".sha256").write_text(f"{digest}  {args.archive.name}\n")
    if args.unpublished:
        check_unpublished(args.platform, version)
    if output := os.environ.get("GITHUB_OUTPUT"):
        with open(output, "a") as stream:
            stream.write(f"version={version}\n")
    print(f"Validated {args.platform} {version}")


if __name__ == "__main__":
    main()
