"""Measure the left edge of each row inside the 任务详情 sheet.

Usage: python3 measure_left_edges.py <render.png> [<render.png> ...]

The sheet's face is a flat fill (the dialog's Background: #18181b in the
dark appearance, #ffffff in the light). Its span is the stretch of that
fill between the first and last fill pixel of the widest row; every row
below is then sampled for the first pixel that differs from the fill. The
output is a pixel inset measured from the frame, not a CSS claim. Two or
more images print a markdown table, which is how the before/after
comparison is checked.
"""
import sys

from PIL import Image

# (name, y from, y to) — bands that cross one row of the sheet in the
# 1280x800 render that TestRenderDetailSizes writes.
ROWS = [
    ("任务详情 header", 168, 190),
    ("任务标题", 209, 232),
    ("任务正文", 247, 268),
    ("移动到列 select", 388, 414),
    ("评论 label", 428, 443),
    ("comment card", 452, 498),
    ("composer", 540, 580),
    ("归档 footer", 604, 640),
]

# Vertical band that holds the sheet. Above it the top bar; below it the
# window edge, whose shadow would merge into one long dark run.
SHEET_Y0, SHEET_Y1 = 160, 640

# The sheet spans ~560 px at 1280 wide; anything narrower is the dimmed
# board showing between two text runs, not the sheet.
MIN_SPAN = 480


def near(a, b, tol=3):
    return all(abs(x - y) <= tol for x, y in zip(a, b))


def run_end(px, x, y, c, w):
    """Index just past the run of colors near c along the row."""
    while x < w and near(px[x, y], c):
        x += 1
    return x


def longest_run(px, w, y, lo, hi):
    """(start, end, color) of the longest run of one color between lo and hi."""
    best = (lo, lo, px[lo, y])
    x = lo
    while x < hi:
        start = x
        c = px[x, y]
        while x < hi and near(px[x, y], c):
            x += 1
        if x - start > best[1] - best[0]:
            best = (start, x - 1, c)
        if x < hi:
            x += 1
    return best


def sheet_fill(px, w, h):
    """The sheet's fill.

    The dialog sits over a dimmed board, so a sample taken at the window
    edge reads the board. The sheet is centered, so only the middle of the
    window is searched; its face is the color of the longest run there.
    """
    lo, hi = w // 4, w - w // 4
    best = (0, lo, None)
    for y in range(SHEET_Y0, SHEET_Y1):
        start, end, c = longest_run(px, w, y, lo, hi)
        if end - start > best[0]:
            best = (end - start, start, c)
    if best[2] is None or best[0] < MIN_SPAN:
        raise SystemExit("no sheet fill found")
    return best[2]


def sheet_span(px, w, h, fill):
    """The sheet's span: first to last fill pixel of its widest unbroken run.

    Taking the outermost fill pixel of a row would stretch the span to the
    window edge whenever the board behind happens to share the fill, so
    the row is read as runs and the widest run is the sheet.
    """
    left, right = w - 1, w - 1
    for y in range(SHEET_Y0, SHEET_Y1):
        x = 0
        while x < w:
            start = x
            while x < w and near(px[x, y], fill):
                x += 1
            if x - start > right - left and x - start > MIN_SPAN:
                left, right = start, x - 1
            if x < w:
                x += 1
    return left, right


def row_left(px, span, y0, y1, fill):
    left, right = span
    for y in range(y0, y1):
        for x in range(left + 1, right):
            if not near(px[x, y], fill):
                return x - left
    return None


def measure(path):
    im = Image.open(path).convert("RGB")
    px = im.load()
    w, h = im.size
    fill = sheet_fill(px, w, h)
    span = sheet_span(px, w, h, fill)
    return fill, span, {name: row_left(px, span, a, b, fill) for name, a, b in ROWS}


def main(paths):
    results = [(p, *measure(p)) for p in paths]
    if len(paths) == 1:
        path, fill, span, rows = results[0]
        print("%s: fill %s, sheet span %s" % (path, fill, span))
        for name, inset in rows.items():
            print("  %-18s inset %s px" % (name, inset))
        return
    print("| 行 | " + " | ".join(p.split("/")[-1] for p in paths) + " |")
    print("| --- |" + " --- |" * len(paths))
    for name, _a, _b in ROWS:
        cells = ["—" if rows[name] is None else "%d px" % rows[name] for _p, _f, _s, rows in results]
        print("| %s | %s |" % (name, " | ".join(cells)))


if __name__ == "__main__":
    main(sys.argv[1:])
