import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { IOS_ONLY, LOCALES, buildTables, toStringsValue } from "./gen-ios-strings.mjs";

const root = path.resolve(import.meta.dirname, "..");

test("string placeholders become positional object specifiers, ints stay", () => {
  assert.equal(toStringsValue("%1$s of %2$s"), "%1$@ of %2$@");
  assert.equal(toStringsValue("%2$d left"), "%2$d left");
});

test("XML entities and Android escapes are unescaped", () => {
  assert.equal(toStringsValue("A &amp; B &lt;c&gt;"), "A & B <c>");
  assert.equal(toStringsValue("Couldn\\'t"), "Couldn't");
});

test("quotes, backslashes and newlines are escaped for .strings", () => {
  assert.equal(toStringsValue('say "hi"'), 'say \\"hi\\"');
  assert.equal(toStringsValue("a\\nb"), "a\\nb");
  assert.equal(toStringsValue("a\\\\b"), "a\\\\b");
});

test("every IOS_ONLY key has all 8 locales", () => {
  assert.equal(LOCALES.length, 8);
  for (const [key, translations] of Object.entries(IOS_ONLY)) {
    for (const code of LOCALES) {
      assert.ok(translations[code], `${key} missing ${code}`);
    }
  }
});

test("every locale table has the same keys", () => {
  const tables = buildTables(root);
  const keys = Object.keys(tables.en);
  for (const code of LOCALES) {
    assert.deepEqual(Object.keys(tables[code]), keys, code);
  }
});

function swiftFiles(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) return swiftFiles(full);
    return entry.name.endsWith(".swift") ? [full] : [];
  });
}

test("every localized key used in Swift exists in the English table", () => {
  const english = buildTables(root).en;
  const used = new Set();
  for (const file of swiftFiles(path.join(root, "apps", "ios"))) {
    const text = fs.readFileSync(file, "utf8");
    for (const m of text.matchAll(/localized:\s*"([^"]+)"/g)) used.add(m[1]);
  }
  assert.ok(used.size > 0);
  for (const key of used) {
    assert.ok(key in english, `missing key ${key}`);
  }
});
