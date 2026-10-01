#!/usr/bin/env python3
"""Dependency-free validation for the generated Astral logo collection."""

from __future__ import annotations

import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

ROOT = Path(__file__).resolve().parent
CONCEPTS = ("constellation", "prism", "modwave")
COLORS = ("white", "black", "primary")
FORMS = ("icon", "lockup")
SURFACES = ("transparent", "tile")
HEX = re.compile(r"^#[0-9a-f]{6}$", re.I)
FORBIDDEN = re.compile(r"oklch\(|var\(|currentColor|NaN|Infinity|<text\b", re.I)
NUMBER = re.compile(r"[-+]?(?:\d+\.?\d*|\.\d+)")


def fail(message: str) -> None:
    print(f"FAIL: {message}", file=sys.stderr)
    raise SystemExit(1)


def expected_paths() -> set[Path]:
    return {
        Path(f"{number:02d}-{concept}/{surface}/{concept}-{color}-{form}.svg")
        for number, concept in enumerate(CONCEPTS, 1)
        for surface in SURFACES
        for color in COLORS
        for form in FORMS
    }


def local_name(tag: str) -> str:
    return tag.rsplit("}", 1)[-1]


def attributes(root: ET.Element) -> str:
    return " ".join(f'{key}="{value}"' for element in root.iter() for key, value in element.attrib.items())


def check_file(path: Path, relative: Path) -> None:
    raw = path.read_text(encoding="utf-8")
    if FORBIDDEN.search(raw):
        fail(f"forbidden token in {relative}")
    try:
        root = ET.fromstring(raw)
    except ET.ParseError as error:
        fail(f"invalid XML in {relative}: {error}")
    if local_name(root.tag) != "svg":
        fail(f"root is not svg in {relative}")
    if "viewBox" not in root.attrib:
        fail(f"missing viewBox in {relative}")
    if relative.parts[-1].endswith("-icon.svg") and root.attrib.get("viewBox") != "0 0 512 512":
        fail(f"icon must use 512 viewBox in {relative}")
    if relative.parts[-2] == "tile" and root.attrib.get("viewBox") != "0 0 512 512":
        fail(f"tile must use 512 viewBox in {relative}")
    if not NUMBER.fullmatch(root.attrib.get("width", "")):
        fail(f"invalid width in {relative}")
    if not NUMBER.fullmatch(root.attrib.get("height", "")):
        fail(f"invalid height in {relative}")

    rects = [element for element in root.iter() if local_name(element.tag) == "rect"]
    is_tile = relative.parts[-2] == "tile"
    full_backgrounds = [
        element for element in rects
        if element.attrib.get("width") == "512"
        and element.attrib.get("height") == "512"
        and element.attrib.get("rx") == "112"
    ]
    if is_tile and len(full_backgrounds) != 1:
        fail(f"expected one rounded tile background in {relative}")
    if not is_tile and full_backgrounds:
        fail(f"transparent asset contains a canvas background in {relative}")

    # All color-bearing paint values must be explicit six-digit hex. The
    # transparent files may have no fill on groups, but never implicit CSS.
    for element in root.iter():
        for key in ("fill", "stroke"):
            value = element.attrib.get(key)
            if value and value != "none" and not HEX.fullmatch(value):
                fail(f"non-hex {key}={value!r} in {relative}")


def main() -> None:
    actual = {path.relative_to(ROOT) for path in ROOT.glob("**/*.svg")}
    expected = expected_paths()
    if actual != expected:
        fail(f"SVG matrix mismatch; missing={sorted(expected - actual)}, extra={sorted(actual - expected)}")
    if len(actual) != 36:
        fail(f"expected 36 SVGs, found {len(actual)}")
    for relative in sorted(actual):
        check_file(ROOT / relative, relative)
    manifest = ROOT / "manifest.json"
    if not manifest.exists():
        fail("manifest.json is missing")
    print(f"OK — validated {len(actual)} SVGs, 3 concepts × 3 colors × 2 forms × 2 surfaces")


if __name__ == "__main__":
    main()
