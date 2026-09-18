// Exports the standalone SVG that the README embeds, from the archify artifact.
//
// GitHub renders README images through <img>, which runs no script and applies
// no external CSS, so the SVG has to carry everything it needs. archify's own
// stylesheet says its variables "also target a standalone exported SVG, whose
// root carries data-preset and data-theme directly", so this is the shape the
// renderer already expects — not a reinterpretation of it.
//
// Run it after every `archify deliver`, or the README image and the interactive
// diagram drift apart:
//
//   node docs/diagrams/export-svg.mjs
//
// Usage: node export-svg.mjs [input.html] [output.svg]

import { readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const input = resolve(process.argv[2] ?? `${here}/architecture.html`);
const output = resolve(process.argv[3] ?? `${here}/architecture.svg`);

const html = readFileSync(input, "utf8");

const svg = html.match(/<svg[\s\S]*<\/svg>/)?.[0];
if (!svg) throw new Error(`no <svg> found in ${input}`);

const css = [...html.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map((m) => m[1]).join("\n");
if (!css) throw new Error(`no <style> found in ${input}`);

// The typeface is applied to `body` in the page. A standalone SVG has no body,
// its root is the <svg>, so nothing matches and every label falls back to the
// user agent's serif. Re-point that one declaration at the root; the stack
// itself is copied verbatim, including archify's embedded @font-face.
const fontStack = css.match(/(?:^|})\s*body\s*\{[^}]*font-family:\s*([^;]+);/)?.[1]?.trim();
if (!fontStack) throw new Error("no body font-family found; the export would render in serif");

// Only add root attributes the artifact does not already carry: XML rejects a
// duplicated attribute outright, and archify already stamps data-preset.
const open = svg.match(/<svg[^>]*>/)[0];
const attrs = [];
if (!/\sxmlns=/.test(open)) attrs.push('xmlns="http://www.w3.org/2000/svg"');
if (!/\sxmlns:xlink=/.test(open)) attrs.push('xmlns:xlink="http://www.w3.org/1999/xlink"');
if (!/\sdata-theme=/.test(open)) attrs.push('data-theme="light"');

// CDATA because the stylesheet contains ">" and "&", which are markup in XML.
const style = `<style type="text/css"><![CDATA[\n${css}\nsvg{font-family:${fontStack};}\n]]></style>`;

const out = svg
  .replace(/^<svg/, `<svg ${attrs.join(" ")}`)
  .replace(/(<svg[^>]*>)/, `$1${style}`);

writeFileSync(output, `<?xml version="1.0" encoding="UTF-8"?>\n${out}`);
console.log(`wrote ${output} (${out.length} bytes)`);
