#!/usr/bin/env python3
"""Generate the memory-manager architecture diagram at three size presets.

One topology, three canvases. The geometry is computed rather than hand-written
so the connector rules can be asserted instead of eyeballed:

  - every connector is a straight horizontal line (endpoints share y), so the
    orthogonal-routing rule is satisfied by construction and nothing crosses
  - every label mask is checked to sit inside its column gap with clearance, and
    to overlap no node rect (the mask would otherwise be clipped by a node fill)
  - every arrow attachment is checked to sit inside both endpoint boxes with
    margin, and connectors sharing an edge are checked for >= 12px separation
"""

import html
import pathlib
import sys

# --- tokens (default skin from references/style-guide.md) --------------------
PAPER = "#f5f5f5"
INK = "#2d3142"
MUTED = "#4f5d75"
ACCENT = "#eb6c36"
SOFT = "rgba(45,49,66,0.45)"
RULE = "rgba(45,49,66,0.10)"

STORE_FILL = "rgba(45,49,66,0.05)"
STORE_STROKE = MUTED
AGENT_FILL = "#ffffff"
AGENT_STROKE = INK
FOCAL_FILL = "rgba(235,108,54,0.10)"
FOCAL_STROKE = ACCENT

# --- size presets -----------------------------------------------------------
# ramp: (node_name, sublabel, arrow_label, tag)
PRESETS = {
    "doc-inline": dict(
        w=960, h=600, ramp=(12, 9, 8, 7),
        cols=[(40, 200), (344, 248), (696, 200)],
        rows=dict(
            project=(88, 80), identity=(216, 72), personal=(328, 88),
            merged=(88, 328), agent=(216, 80),
        ),
        ys=dict(
            p_in=112, p_out=144, id_in=252,
            pe_out=352, pe_in=384, a_out=240, a_in=272,
        ),
        legend_y=452, line_h=14,
    ),
    "doc-wide": dict(
        w=1280, h=720, ramp=(12, 9, 8, 7),
        cols=[(48, 248), (424, 320), (872, 320)],
        rows=dict(
            project=(96, 96), identity=(288, 96), personal=(456, 112),
            merged=(96, 472), agent=(288, 112),
        ),
        ys=dict(
            p_in=128, p_out=168, id_in=336,
            pe_out=488, pe_in=528, a_out=320, a_in=360,
        ),
        legend_y=624, line_h=14,
    ),
    "slide-16x9": dict(
        w=1280, h=720, ramp=(16, 12, 12, 8),
        cols=[(48, 240), (456, 320), (944, 272)],
        rows=dict(
            project=(88, 112), identity=(296, 104), personal=(456, 128),
            merged=(88, 496), agent=(296, 128),
        ),
        ys=dict(
            p_in=128, p_out=176, id_in=348,
            pe_out=496, pe_in=544, a_out=332, a_in=380,
        ),
        legend_y=608, line_h=18,
    ),
}

TITLE = "How memory-manager keys memory by git remote"
DESC = ("Architecture diagram: project memory committed in the work repo and personal memory in a "
        "private repo are merged into the directory Claude Code reads, keyed by the normalized git "
        "remote rather than the filesystem path, and what the session writes is routed back to each "
        "layer by memory type.")


def esc(s):
    return html.escape(s, quote=True)


class Canvas:
    """Accumulates SVG and validates geometry as elements are added."""

    def __init__(self, preset):
        self.p = preset
        self.name_fs, self.sub_fs, self.arrow_fs, self.tag_fs = preset["ramp"]
        self.nodes = {}      # key -> (x, y, w, h)
        self.masks = []      # (x, y, w, h, text) recorded for the overlap check
        self.edge_use = {}   # (node_key, edge) -> [y or x, ...]
        self.arrows = []
        self.boxes = []
        self.errors = []

    # -- geometry helpers ---------------------------------------------------
    def node(self, key, col, row):
        x, w = self.p["cols"][col]
        y, h = self.p["rows"][row]
        self.nodes[key] = (x, y, w, h)
        return x, y, w, h

    def _check_attach(self, key, y, edge):
        x0, y0, w, h = self.nodes[key]
        if not (y0 + 12 <= y <= y0 + h - 12):
            self.errors.append(f"attach y={y} not >=12px inside {key} ({y0}..{y0 + h})")
        used = self.edge_use.setdefault((key, edge), [])
        for other in used:
            if abs(other - y) < 12:
                self.errors.append(f"{key} {edge} edge: attach points {other} and {y} are <12px apart")
        used.append(y)

    def harrow(self, src, dst, y, label, dashed=False):
        """Straight horizontal connector between two nodes, left-to-right or right-to-left."""
        sx, _, sw, _ = self.nodes[src]
        dx, _, dw, _ = self.nodes[dst]
        if sx < dx:
            x1, x2 = sx + sw, dx           # exit right edge, enter left edge
            self._check_attach(src, y, "right")
            self._check_attach(dst, y, "left")
        else:
            x1, x2 = sx, dx + dw           # exit left edge, enter right edge
            self._check_attach(src, y, "left")
            self._check_attach(dst, y, "right")

        stroke = SOFT if dashed else MUTED
        dash = ' stroke-dasharray="4,3"' if dashed else ""
        width = "1" if dashed else "1.2"
        self.arrows.append(
            f'<line x1="{x1}" y1="{y}" x2="{x2}" y2="{y}" stroke="{stroke}" '
            f'stroke-width="{width}"{dash} marker-end="url(#arrow)"/>'
        )
        if label:
            self._label(x1, x2, y, label)

    def _label(self, x1, x2, y, text):
        """Masked arrow label, centred in the corridor, 8px above the stroke."""
        lo, hi = min(x1, x2), max(x1, x2)
        cx = (lo + hi) // 2
        # Geist Mono advance is ~0.6em; add padding for the mask.
        mw = int(len(text) * self.arrow_fs * 0.62) + 16
        mw += mw % 4
        mh = self.arrow_fs + 4
        mh += mh % 4
        mx, my = cx - mw // 2, y - mh - 8

        corridor = hi - lo
        if mw + 16 > corridor:
            self.errors.append(f'label "{text}" needs {mw}px, corridor is only {corridor}px')
        for key, (nx, ny, nw, nh) in self.nodes.items():
            if mx < nx + nw and mx + mw > nx and my < ny + nh and my + mh > ny:
                self.errors.append(f'label "{text}" mask overlaps node {key}')
        for ox, oy, ow, oh, otext in self.masks:
            if mx < ox + ow and mx + mw > ox and my < oy + oh and my + mh > oy:
                self.errors.append(f'label "{text}" mask overlaps label "{otext}"')
        self.masks.append((mx, my, mw, mh, text))

        self.arrows.append(
            f'<rect x="{mx}" y="{my}" width="{mw}" height="{mh}" rx="2" fill="{PAPER}"/>'
            f'<text x="{cx}" y="{my + mh - 4}" fill="{MUTED}" font-size="{self.arrow_fs}" '
            f'font-family="\'Geist Mono\', monospace" text-anchor="middle" '
            f'letter-spacing="0.06em">{esc(text)}</text>'
        )

    def box(self, key, tag, name, subs, kind, split=False):
        x, y, w, h = self.nodes[key]
        fill, stroke = {
            "store": (STORE_FILL, STORE_STROKE),
            "agent": (AGENT_FILL, AGENT_STROKE),
            "focal": (FOCAL_FILL, FOCAL_STROKE),
        }[kind]

        cx = x + w // 2
        line_h = self.p["line_h"]

        # A box tall enough to span several connector rows leaves a large void if
        # everything is centred. Split the copy into an identity block in the
        # upper third and a mechanism block in the lower third, so the node reads
        # as "arrives at the top, leaves at the bottom" like its connectors.
        if split and len(subs) >= 2:
            head, tail = subs[:1], subs[1:]
            head_top = y + h // 4
            tail_top = y + (h * 5) // 8
            placements = [(head_top, name, head), (tail_top, None, tail)]
            lowest = tail_top + len(tail) * line_h
            if lowest > y + h - 12:
                self.errors.append(f"split copy overflows box {key} ({lowest} > {y + h - 12})")
        else:
            block = self.name_fs + len(subs) * line_h
            placements = [(y + (h - block) // 2 + self.name_fs, name, subs)]

        parts = [
            f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="6" fill="{PAPER}"/>',
            f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="6" fill="{fill}" '
            f'stroke="{stroke}" stroke-width="1"/>',
        ]
        tw = int(len(tag) * self.tag_fs * 0.72) + 12
        tw += tw % 4
        parts.append(
            f'<rect x="{x + 8}" y="{y + 8}" width="{tw}" height="12" rx="2" fill="none" '
            f'stroke="{stroke}" stroke-opacity="0.40" stroke-width="0.8"/>'
            f'<text x="{x + 8 + tw // 2}" y="{y + 17}" fill="{stroke}" fill-opacity="0.85" '
            f'font-size="{self.tag_fs}" font-family="\'Geist Mono\', monospace" '
            f'text-anchor="middle" letter-spacing="0.08em">{esc(tag)}</text>'
        )
        for top, heading, lines in placements:
            offset = 0
            if heading:
                parts.append(
                    f'<text x="{cx}" y="{top}" fill="{INK}" font-size="{self.name_fs}" '
                    f'font-weight="600" font-family="\'Geist\', sans-serif" '
                    f'text-anchor="middle">{esc(heading)}</text>'
                )
                offset = 1
                nw = len(heading) * self.name_fs * 0.56
                if nw > w - 24:
                    self.errors.append(
                        f'name "{heading}" is {int(nw)}px wide, box {key} allows {w - 24}px')
            for i, sub in enumerate(lines):
                parts.append(
                    f'<text x="{cx}" y="{top + (i + offset) * line_h}" fill="{MUTED}" '
                    f'font-size="{self.sub_fs}" font-family="\'Geist Mono\', monospace" '
                    f'text-anchor="middle">{esc(sub)}</text>'
                )
                width = len(sub) * self.sub_fs * 0.62
                if width > w - 24:
                    self.errors.append(
                        f'sublabel "{sub}" is {int(width)}px wide, box {key} allows {w - 24}px')
        self.boxes.append("".join(parts))


def legend(c):
    p = c.p
    y = p["legend_y"]
    right = p["w"] - 40
    out = [f'<line x1="40" y1="{y}" x2="{right}" y2="{y}" stroke="{RULE}" stroke-width="0.8"/>']
    out.append(
        f'<text x="40" y="{y + 20}" fill="{MUTED}" font-size="8" '
        f'font-family="\'Geist Mono\', monospace" letter-spacing="0.14em">LEGEND</text>'
    )
    items = [
        ("swatch", STORE_FILL, STORE_STROKE, "MEMORY LAYER, IN GIT"),
        ("swatch", FOCAL_FILL, FOCAL_STROKE, "FOCAL"),
        ("line", None, None, "COMMITTED AND PUSHED"),
        ("dash", None, None, "IN THE WORK TREE, NOT COMMITTED"),
    ]
    x = 120
    step = (right - x) // len(items)
    for kind, fill, stroke, text in items:
        if kind == "swatch":
            out.append(
                f'<rect x="{x}" y="{y + 10}" width="12" height="12" rx="2" fill="{fill}" '
                f'stroke="{stroke}" stroke-width="1"/>'
            )
        else:
            dash = ' stroke-dasharray="4,3"' if kind == "dash" else ""
            colour = SOFT if kind == "dash" else MUTED
            out.append(
                f'<line x1="{x}" y1="{y + 16}" x2="{x + 20}" y2="{y + 16}" stroke="{colour}" '
                f'stroke-width="1.2"{dash}/>'
            )
        out.append(
            f'<text x="{x + 28}" y="{y + 20}" fill="{MUTED}" font-size="8" '
            f'font-family="\'Geist Mono\', monospace" letter-spacing="0.08em">{esc(text)}</text>'
        )
        x += step
    return "".join(out)


def build(preset_name):
    p = PRESETS[preset_name]
    c = Canvas(p)
    ys = p["ys"]

    c.node("project", 0, "project")
    c.node("identity", 0, "identity")
    c.node("personal", 0, "personal")
    c.node("merged", 1, "merged")
    c.node("agent", 2, "agent")

    # Arrows before boxes, so z-order puts strokes behind the nodes.
    c.harrow("project", "merged", ys["p_in"], "MERGE")
    c.harrow("merged", "project", ys["p_out"], "TYPE PROJECT", dashed=True)
    c.harrow("identity", "merged", ys["id_in"], "KEYS BY REMOTE")
    c.harrow("merged", "personal", ys["pe_out"], "COMMIT + PUSH")
    c.harrow("personal", "merged", ys["pe_in"], "GIT PULL")
    c.harrow("merged", "agent", ys["a_out"], "READS")
    c.harrow("agent", "merged", ys["a_in"], "WRITES")

    c.box("project", "LAYER", "Project memory", [
        ".claude/memory/", "committed in the work repo",
    ], "store")
    c.box("identity", "KEY", "Identity", [
        "normalized git remote", "not the filesystem path",
    ], "focal")
    c.box("personal", "LAYER", "Personal memory", [
        "private repo", "global/  projects/<slug>/",
    ], "store")
    c.box("merged", "MERGED", "Claude Code memory directory", [
        "~/.claude/projects/<mangled>/memory/",
        "MEMORY.md regenerated from frontmatter",
        "manifest: layer, origin, content hash",
    ], "focal", split=True)
    c.box("agent", "AGENT", "Claude Code session", [
        "reads at start", "writes as it works",
    ], "agent")

    if c.errors:
        for e in c.errors:
            print(f"  GEOMETRY FAIL [{preset_name}]: {e}", file=sys.stderr)
        return None

    slug = f"memory-manager-{preset_name}"
    svg = f'''<svg viewBox="0 0 {p["w"]} {p["h"]}" xmlns="http://www.w3.org/2000/svg" role="img" aria-labelledby="{slug}-title {slug}-desc">
      <title id="{slug}-title">{esc(TITLE)}</title>
      <desc id="{slug}-desc">{esc(DESC)}</desc>
      <defs>
        <marker id="arrow" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto"><polygon points="0 0, 8 3, 0 6" fill="{MUTED}"/></marker>
        <marker id="arrow-accent" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto"><polygon points="0 0, 8 3, 0 6" fill="{ACCENT}"/></marker>
        <marker id="arrow-link" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto"><polygon points="0 0, 8 3, 0 6" fill="#2e5aa8"/></marker>
      </defs>

      <rect width="100%" height="100%" fill="{PAPER}"/>

      {"".join(c.arrows)}

      {"".join(c.boxes)}

      {legend(c)}
    </svg>'''

    min_w = 900 if p["w"] < 1100 else 1100
    return f'''<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{esc(TITLE)}</title>
  <link href="https://fonts.googleapis.com/css2?family=Instrument+Serif:ital@0;1&family=Geist:wght@400;500;600&family=Geist+Mono:wght@400;500;600&display=swap" rel="stylesheet">
  <style>
    *, *::before, *::after {{ box-sizing: border-box; margin: 0; padding: 0; }}
    :root {{
      --color-paper:  {PAPER};
      --color-ink:    {INK};
      --color-muted:  {MUTED};
      --color-accent: {ACCENT};
      --font-sans:  'Geist', system-ui, sans-serif;
      --font-serif: 'Instrument Serif', serif;
      --font-mono:  'Geist Mono', ui-monospace, monospace;
    }}
    body {{
      font-family: var(--font-sans);
      background: var(--color-paper);
      color: var(--color-ink);
      min-height: 100vh;
      display: flex;
      align-items: center;
      justify-content: center;
      padding: 3rem 2rem;
    }}
    .frame {{ max-width: {p["w"] + 240}px; width: 100%; }}
    .eyebrow {{
      font-family: var(--font-mono);
      font-size: 0.66rem;
      font-weight: 500;
      letter-spacing: 0.18em;
      text-transform: uppercase;
      color: var(--color-muted);
      margin-bottom: 0.5rem;
    }}
    h1 {{
      font-family: var(--font-serif);
      font-size: clamp(1.5rem, 2.4vw + 0.75rem, {"2.5rem" if preset_name == "slide-16x9" else "1.75rem"});
      font-weight: 400;
      letter-spacing: -0.02em;
      line-height: 1.15;
      color: var(--color-ink);
      margin-bottom: 1.5rem;
    }}
    svg {{ width: 100%; min-width: {min_w}px; display: block; }}
  </style>
</head>
<body>
  <div class="frame">
    <p class="eyebrow">Architecture · {preset_name}</p>
    <h1>{esc(TITLE)}</h1>
    {svg}
  </div>
</body>
</html>
'''


def main():
    out_dir = pathlib.Path(sys.argv[1])
    out_dir.mkdir(parents=True, exist_ok=True)
    failed = False
    for name in PRESETS:
        doc = build(name)
        if doc is None:
            failed = True
            continue
        path = out_dir / f"architecture-{name}.html"
        path.write_text(doc, encoding="utf-8")
        print(f"wrote {path}")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
