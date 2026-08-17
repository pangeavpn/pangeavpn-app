import test from "node:test";
import assert from "node:assert/strict";
import { ACCENT_THEMES, ACCENT_NAMES, DEFAULT_ACCENT, isAccentName, resolveAccent, swatchColor } from "./accentThemes.ts";

test("terra stays the default and keeps its shipped values", () => {
  assert.equal(DEFAULT_ACCENT, "terra");
  assert.deepEqual(ACCENT_THEMES.terra, { rgb: "195 86 43", hover: "#a84a25" });
});

test("every preset carries a space-separated rgb triple and a hover hex", () => {
  assert.deepEqual(ACCENT_NAMES, ["terra", "purple", "ocean", "emerald", "rose"]);
  for (const name of ACCENT_NAMES) {
    const theme = ACCENT_THEMES[name];
    assert.match(theme.rgb, /^\d{1,3} \d{1,3} \d{1,3}$/, `${name} rgb`);
    assert.match(theme.hover, /^#[0-9a-f]{6}$/, `${name} hover`);
  }
});

test("recognizes only shipped preset names", () => {
  assert.equal(isAccentName("purple"), true);
  assert.equal(isAccentName("terra"), true);
  assert.equal(isAccentName("chartreuse"), false);
  assert.equal(isAccentName(""), false);
  assert.equal(isAccentName(null), false);
  assert.equal(isAccentName(42), false);
});

test("falls back to the default for missing or corrupt stored values", () => {
  assert.equal(resolveAccent("ocean"), "ocean");
  assert.equal(resolveAccent(null), DEFAULT_ACCENT);
  assert.equal(resolveAccent(undefined), DEFAULT_ACCENT);
  assert.equal(resolveAccent("{}"), DEFAULT_ACCENT);
  assert.equal(resolveAccent("Purple"), DEFAULT_ACCENT);
});

test("renders a preset as a css color for swatch buttons", () => {
  assert.equal(swatchColor("terra"), "rgb(195 86 43)");
  assert.equal(swatchColor("rose"), `rgb(${ACCENT_THEMES.rose.rgb})`);
});
