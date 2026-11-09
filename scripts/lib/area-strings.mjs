// Loads a folder of per-area string files: each default-exports { key: { en, es, ... } }.
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

export async function loadAreaStrings(dir, taken = {}) {
  const merged = {};
  if (!fs.existsSync(dir)) return merged;
  const files = fs.readdirSync(dir).filter((name) => name.endsWith(".mjs")).sort();
  for (const name of files) {
    const { default: entries } = await import(pathToFileURL(path.join(dir, name)).href);
    for (const [key, translations] of Object.entries(entries)) {
      if (key in merged || key in taken) throw new Error(`Key "${key}" is defined twice (${name})`);
      merged[key] = translations;
    }
  }
  return merged;
}
