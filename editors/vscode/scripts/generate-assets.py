"""生成 VS Code 插件图标和 README 语法高亮预览图。

只使用 Pillow；生成结果是仓库内已有 Mio 语言标识的位图派生，
不会改动或重绘 Mio / Mochi 角色插画。
"""

from __future__ import annotations

import os
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parents[1]
ASSETS = ROOT / "assets"
IMAGES = ROOT / "docs" / "images"
ASSETS.mkdir(parents=True, exist_ok=True)
IMAGES.mkdir(parents=True, exist_ok=True)

MENLO = "/System/Library/Fonts/Menlo.ttc"
PINGFANG = "/System/Library/Fonts/Hiragino Sans GB.ttc"


def round_rect(draw, box, radius, fill):
    draw.rounded_rectangle(box, radius=radius, fill=fill)


def build_icon() -> None:
    scale = 64
    size = 16 * scale
    image = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(image)

    # 对应 editors/jetbrains/src/main/resources/icons/mio.svg 的圆角方块。
    round_rect(
        draw,
        (1 * scale, 1 * scale, 15 * scale, 15 * scale),
        3 * scale,
        "#8A63D2",
    )

    # SVG 路径 M3 11V5h2l3 3 3-3h2v6h-2V7.6L8 10.4 5 7.6V11z 的顶点。
    points = [
        (3, 11),
        (3, 5),
        (5, 5),
        (8, 8),
        (11, 5),
        (13, 5),
        (13, 11),
        (11, 11),
        (11, 7.6),
        (8, 10.4),
        (5, 7.6),
        (5, 11),
    ]
    draw.polygon([(x * scale, y * scale) for x, y in points], fill="#FFFFFF")

    icon = image.resize((128, 128), Image.Resampling.LANCZOS)
    icon.save(ROOT / "icon.png")

    # 语言文件图标也复用同一个 M 标识。
    (ASSETS / "mio.svg").write_text(
        """<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
  <rect x="1" y="1" width="14" height="14" rx="3" fill="#8A63D2"/>
  <path d="M3 11V5h2l3 3 3-3h2v6h-2V7.6L8 10.4 5 7.6V11z" fill="#FFFFFF"/>
</svg>
""",
        encoding="utf-8",
    )


def font_for(text: str, size: int):
    if any(ord(character) > 0x7F for character in text):
        try:
            return ImageFont.truetype(PINGFANG, size, index=0)
        except OSError:
            pass
    try:
        return ImageFont.truetype(MENLO, size, index=0)
    except OSError:
        return ImageFont.load_default()


def build_preview(dark: bool) -> None:
    width, height = 1120, 660
    image = Image.new("RGBA", (width, height), (0, 0, 0, 0))
    draw = ImageDraw.Draw(image)

    background = "#1E1E1E" if dark else "#FFFFFF"
    panel = "#252526" if dark else "#F3F3F3"
    border = "#3C3C3C" if dark else "#E0E0E0"
    text = "#D4D4D4" if dark else "#1F1F1F"
    muted = "#858585" if dark else "#6E6E6E"
    keyword_control = "#C586C0" if dark else "#AF00DB"
    keyword_decl = "#569CD6" if dark else "#0000FF"
    type_color = "#4EC9B0" if dark else "#267F99"
    function = "#DCDCAA" if dark else "#795E26"
    string = "#CE9178" if dark else "#A31515"
    number = "#B5CEA8" if dark else "#098658"
    comment = "#6A9955" if dark else "#008000"
    property_color = "#9CDCFE" if dark else "#001080"
    operator = "#D4D4D4" if dark else "#333333"
    punctuation = "#D4D4D4" if dark else "#333333"

    round_rect(draw, (0, 0, width, height), 18, background)
    draw.rectangle((0, 0, width, 54), fill=panel)
    draw.line((0, 54, width, 54), fill=border, width=1)
    for index, color in enumerate(("#FF5F57", "#FEBC2E", "#28C840")):
        draw.ellipse(
            (24 + index * 26, 20, 40 + index * 26, 36),
            fill=color if dark else color,
        )
    title_font = font_for("hello.mio", 18)
    draw.text((140, 17), "hello.mio — Mio Language Support", font=title_font, fill=muted)

    code_size = 24
    code_font = font_for("M", code_size)
    line_height = 38
    left = 82
    top = 94

    lines = [
        [
            ("yobu", keyword_decl),
            (" ", text),
            ('"kiso/math"', string),
            (";", punctuation),
        ],
        [],
        [
            ("koukai", keyword_decl),
            (" ", text),
            ("waza", keyword_decl),
            (" ", text),
            ("max", function),
            ("(", punctuation),
            ("left", property_color),
            (":", punctuation),
            (" ", text),
            ("int64", type_color),
            (",", punctuation),
            (" ", text),
            ("right", property_color),
            (":", punctuation),
            (" ", text),
            ("int64", type_color),
            (")", punctuation),
            (" ", text),
            ("->", operator),
            (" ", text),
            ("int64", type_color),
            (" {", punctuation),
        ],
        [
            ("    ", text),
            ("moshi", keyword_control),
            (" (", punctuation),
            ("left", property_color),
            (" > ", operator),
            ("right", property_color),
            (") {", punctuation),
        ],
        [
            ("        ", text),
            ("kaesu", keyword_control),
            (" ", text),
            ("left", property_color),
            (";", punctuation),
        ],
        [
            ("    ", text),
            ("} ", punctuation),
            ("sore", keyword_control),
            (" {", punctuation),
        ],
        [
            ("        ", text),
            ("kaesu", keyword_control),
            (" ", text),
            ("right", property_color),
            (";", punctuation),
        ],
        [
            ("    ", text),
            ("}", punctuation),
        ],
        [
            ("}", punctuation),
        ],
        [],
        [
            ("kaku", keyword_decl),
            (" ", text),
            ("message", property_color),
            (" = ", operator),
            ('"こんにちは, Mio!"', string),
            (";", punctuation),
        ],
        [
            ("kaku", keyword_decl),
            (" ", text),
            ("symbol", property_color),
            (": ", punctuation),
            ("rune", type_color),
            (" = ", operator),
            ("'美'", string),
            (";", punctuation),
        ],
        [
            ("// 语法高亮、编译器诊断与 CLI 集成", comment),
        ],
    ]

    for line_index, segments in enumerate(lines):
        y = top + line_index * line_height
        line_number = str(line_index + 1).rjust(2)
        draw.text((32, y), line_number, font=code_font, fill=muted)
        x = left
        for segment, color in segments:
            segment_font = font_for(segment, code_size)
            draw.text((x, y), segment, font=segment_font, fill=color)
            x += draw.textlength(segment, font=segment_font)

    # 底部状态栏。
    draw.rectangle((0, height - 38, width, height), fill=panel)
    draw.line((0, height - 38, width, height - 38), fill=border, width=1)
    status_font = font_for("Mio", 15)
    draw.text((24, height - 30), "Mio 1.0.0-beta.1", font=status_font, fill=muted)
    draw.text((width - 150, height - 30), "UTF-8  Mio", font=status_font, fill=muted)

    output = IMAGES / ("syntax-preview-dark.png" if dark else "syntax-preview-light.png")
    image.save(output)


def main() -> None:
    build_icon()
    build_preview(True)
    build_preview(False)
    for path in [ROOT / "icon.png", IMAGES / "syntax-preview-dark.png", IMAGES / "syntax-preview-light.png"]:
        print(f"generated {path.relative_to(ROOT)} ({path.stat().st_size} bytes)")


if __name__ == "__main__":
    main()
