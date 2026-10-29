import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import {
  IOS_ONLY, LOCALES, buildAll, buildTables, parsePlural, renderStringsdict, toStringsValue
} from "./gen-ios-strings.mjs";

const root = path.resolve(import.meta.dirname, "..");

const ANDROID_DIRS = [
  "values", "values-es", "values-fr", "values-ru", "values-uk", "values-zh", "values-ar", "values-fa"
];

function makeFixture(files) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gen-ios-strings-"));
  for (const mod of ["ui-common", "core"]) {
    for (const values of ANDROID_DIRS) {
      const body = (files[mod] ?? {})[values] ?? "";
      const resDir = path.join(dir, "apps", "android", mod, "src", "main", "res", values);
      fs.mkdirSync(resDir, { recursive: true });
      fs.writeFileSync(path.join(resDir, "strings.xml"), `<resources>\n${body}\n</resources>\n`);
    }
  }
  return dir;
}

function everyLocale(xml) {
  return Object.fromEntries(ANDROID_DIRS.map((d) => [d, xml]));
}

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

test("kill switch uses the reviewed fr and fa wording", () => {
  assert.equal(IOS_ONLY.settings_kill_switch.fr, "Coupure de sécurité");
  assert.equal(IOS_ONLY.settings_kill_switch.fa, "کلید قطع اضطراری");
});

test("every locale table has the same keys", () => {
  const tables = buildTables(root);
  const keys = Object.keys(tables.en);
  for (const code of LOCALES) {
    assert.deepEqual(Object.keys(tables[code]), keys, code);
  }
});

test("@plural parse keeps categories and rewrites the count to %d", () => {
  const forms = parsePlural("@plural:one=%1$s сервер · авто|few=%1$s сервера|other=%1$s серверов");
  assert.deepEqual(forms, { one: "%d сервер · авто", few: "%d сервера", other: "%d серверов" });
  assert.throws(() => parsePlural("@plural:one=a"), /other/);
  assert.throws(() => parsePlural("@plural:bogus=a|other=b"), /Bad plural/);
});

test("plural keys come out of .strings and into the stringsdict for every locale", () => {
  const { tables, plurals } = buildAll(root);
  for (const code of LOCALES) {
    assert.ok(!("region_auto_of" in tables[code]), `${code} .strings still has region_auto_of`);
    assert.ok("region_auto_of" in plurals[code], `${code} stringsdict lacks region_auto_of`);
  }
  assert.deepEqual(plurals.en.region_auto_of, { other: "%d servers · auto" });
  assert.ok("few" in plurals.ru.region_auto_of);
  assert.ok("zero" in plurals.ar.region_auto_of);
});

test("stringsdict has the plural-rule structure and escapes XML", () => {
  const xml = renderStringsdict({ some_key: { one: "%d <a> & b", other: "%d c" } });
  assert.ok(xml.includes("<key>NSStringLocalizedFormatKey</key>\n\t\t<string>%#@count@</string>"));
  assert.ok(xml.includes("<string>NSStringPluralRuleType</string>"));
  assert.ok(xml.includes("<key>NSStringFormatValueTypeKey</key>\n\t\t\t<string>d</string>"));
  assert.ok(xml.includes("<string>%d &lt;a&gt; &amp; b</string>"));
  assert.ok(xml.indexOf("<key>one</key>") < xml.indexOf("<key>other</key>"));
});

test("a non-plural locale value falls back to other with %d", () => {
  const key = (xml) => `<string name="n">${xml}</string>`;
  const dir = makeFixture({
    "ui-common": {
      ...everyLocale(key("%1$s items")),
      "values-ru": key("@plural:one=%1$s штука|other=%1$s штук")
    }
  });
  try {
    const { tables, plurals } = buildAll(dir, {});
    assert.deepEqual(plurals.en.n, { other: "%d items" });
    assert.deepEqual(plurals.ru.n, { one: "%d штука", other: "%d штук" });
    for (const code of LOCALES) assert.ok(!("n" in tables[code]));
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("a duplicate key across ui-common and core throws", () => {
  const dir = makeFixture({
    "ui-common": everyLocale('<string name="dup">a</string>'),
    core: everyLocale('<string name="dup">b</string>')
  });
  try {
    assert.throws(() => buildTables(dir, {}), /more than one module/);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test("an IOS_ONLY key colliding with an Android key throws", () => {
  const dir = makeFixture({ "ui-common": everyLocale('<string name="clash">a</string>') });
  const iosOnly = { clash: Object.fromEntries(LOCALES.map((c) => [c, "x"])) };
  try {
    assert.throws(() => buildTables(dir, iosOnly), /collides with an Android key/);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

function swiftFiles(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) return swiftFiles(full);
    return entry.name.endsWith(".swift") ? [full] : [];
  });
}

const SNAKE_KEY = /^[a-z][a-z0-9]*(_[a-z0-9]+)+$/;
const LITERAL_CALLS = /\b(?:Text|LocalizedStringKey)\(\s*"([^"]+)"/g;

test("every localized key used in Swift exists in the English table", () => {
  const { tables, plurals } = buildAll(root);
  const known = new Set([...Object.keys(tables.en), ...Object.keys(plurals.en)]);
  const used = new Set();
  for (const file of swiftFiles(path.join(root, "apps", "ios"))) {
    const text = fs.readFileSync(file, "utf8");
    for (const m of text.matchAll(/localized:\s*"([^"]+)"/g)) used.add(m[1]);
    for (const m of text.matchAll(LITERAL_CALLS)) if (SNAKE_KEY.test(m[1])) used.add(m[1]);
  }
  assert.ok(used.size > 0);
  for (const key of used) {
    assert.ok(known.has(key), `missing key ${key}`);
  }
});
