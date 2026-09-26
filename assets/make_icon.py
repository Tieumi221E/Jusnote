"""Builds the app icon from the artwork (assets/icon-source.png).

The artwork is an opaque RGB image: the glass tile sits on a white canvas
with a soft drop shadow. The tile is cut out along its own edge (a rounded
rectangle, fitted to the artwork: left 174, top 190, right 1080, bottom
1074, corner radius 208) with an anti-aliased mask, so it has no white square
around it on dark taskbars, and padded to a square. Outputs:

  assets/icon-<n>.png       for the Windows icon sizes (read by tools/winres)
  web/src/icon-64.png       for the identity pill and the welcome card

and the same from the night artwork (icon-source-dark.png: the same tile as
navy glass on a dark canvas; left 178, top 184, right 1074, bottom 1070,
radius 212) as icon-dark-<n>.png and icon-64-dark.png: the shell and the
pages show the one for the theme.
"""
import os
from PIL import Image, ImageDraw, ImageEnhance

HERE = os.path.dirname(os.path.abspath(__file__))
BOX = (174, 190, 1080, 1074)
RADIUS = 208
SS = 4  # mask supersampling
SIZES = [16, 20, 24, 32, 40, 48, 64, 96, 128, 256]


def make(src: str, suffix: str, edge_rgb: tuple, BOX: tuple, RADIUS: int) -> None:
    art = Image.open(os.path.join(HERE, src)).convert("RGB")
    l, t, r, b = BOX
    w, h = r - l, b - t
    mask = Image.new("L", (w * SS, h * SS), 0)
    ImageDraw.Draw(mask).rounded_rectangle((0, 0, w * SS - 1, h * SS - 1), radius=RADIUS * SS, fill=255)
    mask = mask.resize((w, h), Image.LANCZOS)
    tile = art.crop(BOX).convert("RGBA")
    tile.putalpha(mask)

    side = max(w, h)
    margin = round(side * 0.02)
    canvas = Image.new("RGBA", (side + 2 * margin, side + 2 * margin), (0, 0, 0, 0))
    canvas.paste(tile, (margin + (side - w) // 2, margin + (side - h) // 2), tile)

    def scaled(n: int) -> Image.Image:
        im = canvas.resize((n, n), Image.LANCZOS, reducing_gap=3.0)
        if n > 48:
            return im
        # Small sizes (taskbar, title bar, Explorer lists): the glass tile
        # fades into the background and the note mark washes out. Stronger
        # colour and contrast, and a thin edge, keep it readable; the large
        # sizes stay exactly as drawn.
        a = im.getchannel("A")
        rgb = im.convert("RGB")
        rgb = ImageEnhance.Color(rgb).enhance(1.4)
        rgb = ImageEnhance.Contrast(rgb).enhance(1.15)
        im = rgb.convert("RGBA")
        im.putalpha(a)
        edge = Image.new("L", (n * SS, n * SS), 0)
        m = margin * n * SS / canvas.size[0]
        rr = RADIUS * n * SS / canvas.size[0]
        ImageDraw.Draw(edge).rounded_rectangle((m, m, n * SS - 1 - m, n * SS - 1 - m), radius=rr, outline=255, width=max(SS, round(SS * n / 32)))
        edge = edge.resize((n, n), Image.LANCZOS)
        line = Image.new("RGBA", (n, n), edge_rgb + (0,))
        line.putalpha(edge.point(lambda v: v * 150 // 255))
        return Image.alpha_composite(im, line)

    for n in SIZES:
        scaled(n).save(os.path.join(HERE, f"icon{suffix}-{n}.png"), optimize=True)
    scaled(64).save(os.path.join(HERE, "..", "web", "src", f"icon-64{suffix}.png"), optimize=True)
    print("ok", src, canvas.size)


make("icon-source.png", "", (70, 110, 230), BOX, RADIUS)
make("icon-source-dark.png", "-dark", (130, 160, 255), (178, 184, 1074, 1070), 212)
