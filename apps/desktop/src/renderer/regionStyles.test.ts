import test from "node:test";
import assert from "node:assert/strict";
import {
  DEFAULT_REGION_STYLE,
  REGION_STYLES,
  isRegionStyle,
  pickerAlwaysReachable,
  resolveRegionStyle
} from "./regionStyles.ts";

test("tiles is the default and leads the settings list", () => {
  assert.equal(DEFAULT_REGION_STYLE, "tiles");
  assert.equal(REGION_STYLES[0], "tiles");
  assert.deepEqual(REGION_STYLES, ["tiles", "classic", "ledger", "chips", "sheet", "poster", "drawer"]);
});

test("recognizes only shipped style names", () => {
  assert.equal(isRegionStyle("ledger"), true);
  assert.equal(isRegionStyle("classic"), true);
  assert.equal(isRegionStyle("Tiles"), false);
  assert.equal(isRegionStyle(""), false);
  assert.equal(isRegionStyle(null), false);
  assert.equal(isRegionStyle(3), false);
});

test("falls back to the default for missing or corrupt stored values", () => {
  assert.equal(resolveRegionStyle("poster"), "poster");
  assert.equal(resolveRegionStyle(null), DEFAULT_REGION_STYLE);
  assert.equal(resolveRegionStyle(undefined), DEFAULT_REGION_STYLE);
  assert.equal(resolveRegionStyle("grid"), DEFAULT_REGION_STYLE);
});

test("only the drawer keeps the full list reachable regardless of region count", () => {
  for (const style of REGION_STYLES) assert.equal(pickerAlwaysReachable(style), style === "drawer", style);
});
