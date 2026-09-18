# Diagrams

The architecture diagram at three size presets. Each preset ships as a standalone `.html` (open it
in any browser) and a portable `.svg`.

| File | viewBox | Type ramp | For |
|---|---|---|---|
| `architecture-doc-inline` | 960×600 | standard | README and docs embeds |
| `architecture-doc-wide` | 1280×720 | standard | full-width wiki pages |
| `architecture-slide-16x9` | 1280×720 | presentation (16px names) | projected decks |

Built with the [diagram-design](https://github.com/cathrynlavery/diagram-design) Claude Code plugin,
on a slate-and-teal skin: cool paper `#f7f9fa`, ink `#1f2937`, teal accent `#0f766e`. The tokens are
the semantic roles the plugin's style guide defines, so the accent still marks exactly one focal
node — the merged directory — and nothing else. The shipped default is white-smoke paper with an
atomic-tangerine accent; teal reads technical rather than promotional.

## Regenerating

`generate.py` is the source of truth — edit it, not the HTML. One topology feeds all three presets,
and the geometry is computed rather than hand-written so the connector rules can be *asserted*
instead of eyeballed:

- every connector is a straight horizontal line, so nothing crosses or overlaps by construction
- every label mask is checked to fit its column corridor and to overlap no node (a mask landing
  inside a node gets clipped by the node fill, and the text renders as a fragment)
- every arrow attachment is checked to sit ≥12px inside both endpoint boxes, and connectors sharing
  an edge are checked to be ≥12px apart
- every node name and sublabel is checked to fit its box width

The script exits non-zero and prints what failed, so a bad edit cannot produce a broken diagram.

```sh
python docs/diagrams/generate.py docs/diagrams
```

Then re-export the SVGs:

```sh
python - <<'PY'
import pathlib, re
FONTS = ("@import url('https://fonts.googleapis.com/css2?"
         "family=Instrument+Serif:ital@0;1&amp;family=Geist:wght@400;500;600"
         "&amp;family=Geist+Mono:wght@400;500;600&amp;display=swap');")
for src in sorted(pathlib.Path('docs/diagrams').glob('architecture-*.html')):
    svg = re.search(r'<svg\b.*?</svg>', src.read_text(encoding='utf-8'), re.S).group(0)
    svg = svg.replace('<defs>', f'<defs>\n        <style>{FONTS}</style>', 1)
    src.with_suffix('.svg').write_text('<?xml version="1.0" encoding="UTF-8"?>\n' + svg + '\n', encoding='utf-8')
PY
```

For PNG, the diagram-design plugin's export needs Playwright:

```sh
pip install playwright && playwright install chromium
```

Then `/diagram-design:export-diagram docs/diagrams/architecture-doc-wide.html`.
