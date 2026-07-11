// Verifies the localisation catalogues beyond what the TypeScript compiler
// checks (tsc already guarantees key completeness via `satisfies Messages`):
//   1. no empty string values in any locale
//   2. every key's {placeholder} set matches the English source
//   3. every data-i18n* key used in index.html exists in the catalogue
// Run: node scripts/verify-i18n.mjs   (exits non-zero on any failure)

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const appDir = path.resolve(scriptDir, "..");
const rendererLocalesDir = path.join(appDir, "src/renderer/i18n/locales");
const mainI18nFile = path.join(appDir, "src/main/i18n/index.ts");
const htmlFile = path.join(appDir, "src/renderer/index.html");

const RENDERER_LOCALES = ["en", "es", "fr", "ru", "uk", "zh", "ar", "fa"];

const errors = [];
const warnings = [];

// Evaluate a `export const X = { ... } satisfies/as const` object literal.
function evalObjectLiteral(source, varName) {
  let body = source;
  body = body.replace(/^\s*import[^\n]*\n/gm, "");
  const re = new RegExp(`export const ${varName}\\s*=\\s*`);
  body = body.replace(re, "return ");
  body = body.replace(/\ssatisfies\s+\w+/g, "");
  body = body.replace(/\sas const/g, "");
  // eslint-disable-next-line no-new-func
  return new Function(body)();
}

function placeholders(value) {
  const set = new Set();
  for (const m of value.matchAll(/\{(\w+)\}/g)) set.add(m[1]);
  return [...set].sort();
}

function eq(a, b) {
  return a.length === b.length && a.every((v, i) => v === b[i]);
}

// ── Renderer catalogues ──────────────────────────────────
const catalogues = {};
for (const code of RENDERER_LOCALES) {
  const file = path.join(rendererLocalesDir, `${code}.ts`);
  if (!fs.existsSync(file)) {
    errors.push(`missing renderer locale file: ${code}.ts`);
    continue;
  }
  try {
    catalogues[code] = evalObjectLiteral(fs.readFileSync(file, "utf8"), code);
  } catch (err) {
    errors.push(`could not parse ${code}.ts: ${err.message}`);
  }
}

const en = catalogues.en;
if (!en) {
  console.error("FATAL: en.ts could not be loaded");
  process.exit(1);
}
const enKeys = Object.keys(en);

for (const code of RENDERER_LOCALES) {
  const cat = catalogues[code];
  if (!cat) continue;
  const keys = Object.keys(cat);

  const missing = enKeys.filter((k) => !(k in cat));
  const extra = keys.filter((k) => !(k in en));
  if (missing.length) errors.push(`${code}: missing ${missing.length} key(s): ${missing.slice(0, 5).join(", ")}${missing.length > 5 ? "…" : ""}`);
  if (extra.length) errors.push(`${code}: ${extra.length} extra key(s): ${extra.slice(0, 5).join(", ")}${extra.length > 5 ? "…" : ""}`);

  for (const [key, value] of Object.entries(cat)) {
    if (typeof value !== "string" || value.trim() === "") {
      errors.push(`${code}: empty value for "${key}"`);
      continue;
    }
    if (key in en) {
      const enPh = placeholders(en[key]);
      const ph = placeholders(value);
      if (!eq(enPh, ph)) {
        errors.push(`${code}: placeholder mismatch for "${key}" — en{${enPh.join(",")}} vs ${code}{${ph.join(",")}}`);
      }
    }
  }
}

// ── Main-process catalogues ──────────────────────────────
if (fs.existsSync(mainI18nFile)) {
  const mainSrc = fs.readFileSync(mainI18nFile, "utf8");
  const mainCats = {};
  for (const m of mainSrc.matchAll(/const (\w+): MainMessages = (\{[\s\S]*?\n\});/g)) {
    try {
      // eslint-disable-next-line no-new-func
      mainCats[m[1]] = new Function(`return ${m[2]}`)();
    } catch (err) {
      errors.push(`main: could not parse catalogue ${m[1]}: ${err.message}`);
    }
  }
  const mainEn = mainCats.en;
  if (mainEn) {
    const mainEnKeys = Object.keys(mainEn);
    for (const [code, cat] of Object.entries(mainCats)) {
      const missing = mainEnKeys.filter((k) => !(k in cat));
      if (missing.length) errors.push(`main/${code}: missing key(s): ${missing.join(", ")}`);
      for (const [key, value] of Object.entries(cat)) {
        if (typeof value !== "string" || value.trim() === "") errors.push(`main/${code}: empty value for "${key}"`);
        if (key in mainEn && !eq(placeholders(mainEn[key]), placeholders(value))) {
          errors.push(`main/${code}: placeholder mismatch for "${key}"`);
        }
      }
    }
  } else {
    warnings.push("main: could not locate English catalogue for verification");
  }
}

// ── HTML data-i18n* key existence ────────────────────────
if (fs.existsSync(htmlFile)) {
  const html = fs.readFileSync(htmlFile, "utf8");
  const used = new Set();
  for (const m of html.matchAll(/data-i18n(?:-[a-z-]+)?="([^"]+)"/g)) used.add(m[1]);
  for (const key of used) {
    if (!(key in en)) errors.push(`index.html references unknown i18n key "${key}"`);
  }
  console.log(`HTML: ${used.size} annotated i18n keys checked.`);
}

// ── Report ───────────────────────────────────────────────
console.log(`Renderer: ${enKeys.length} keys × ${RENDERER_LOCALES.length} locales checked.`);
for (const w of warnings) console.log(`  warn: ${w}`);

if (errors.length) {
  console.error(`\n✗ i18n verification failed (${errors.length} error(s)):`);
  for (const e of errors) console.error(`  - ${e}`);
  process.exit(1);
}
console.log("\n✓ i18n verification passed.");
