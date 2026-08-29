import test from "node:test";
import assert from "node:assert/strict";
import { anchorPosition, panelEdge, pickDisplay, samePoint, type AnchorDisplay, type AnchorInput } from "./windowAnchor.ts";

const size = { width: 640, height: 440 };

function display(bounds: AnchorDisplay["bounds"], workArea: AnchorDisplay["workArea"]): AnchorDisplay {
  return { bounds, workArea };
}

// 1920x1080 with a 48px panel on the given edge.
function panelled(edge: "top" | "bottom" | "left" | "right", offsetX = 0): AnchorDisplay {
  const bounds = { x: offsetX, y: 0, width: 1920, height: 1080 };
  switch (edge) {
    case "top":
      return display(bounds, { x: offsetX, y: 48, width: 1920, height: 1032 });
    case "bottom":
      return display(bounds, { x: offsetX, y: 0, width: 1920, height: 1032 });
    case "left":
      return display(bounds, { x: offsetX + 48, y: 0, width: 1872, height: 1080 });
    case "right":
      return display(bounds, { x: offsetX, y: 0, width: 1872, height: 1080 });
  }
}

function input(overrides: Partial<AnchorInput> & Pick<AnchorInput, "primary">): AnchorInput {
  return {
    displays: [overrides.primary],
    cursor: null,
    trayBounds: null,
    size,
    platform: "linux",
    ...overrides
  };
}

test("reads the panel edge off the work-area insets", () => {
  assert.equal(panelEdge(panelled("bottom"), "linux"), "bottom");
  assert.equal(panelEdge(panelled("top"), "linux"), "top");
  assert.equal(panelEdge(panelled("left"), "linux"), "left");
  assert.equal(panelEdge(panelled("right"), "linux"), "right");
});

test("falls back to the bottom when nothing is reserved", () => {
  const full = display({ x: 0, y: 0, width: 1920, height: 1080 }, { x: 0, y: 0, width: 1920, height: 1080 });
  assert.equal(panelEdge(full, "linux"), "bottom");
});

test("ignores the Dock on macOS and stays under the menu bar", () => {
  const dockBottom = display({ x: 0, y: 0, width: 1440, height: 900 }, { x: 0, y: 25, width: 1440, height: 795 });
  assert.equal(panelEdge(dockBottom, "darwin"), "top");
});

test("hugs a bottom panel on Linux", () => {
  const pos = anchorPosition(input({ primary: panelled("bottom") }));
  assert.deepEqual(pos, { x: 1920 - 640, y: 1032 - 440 });
});

test("hugs a top panel on Linux (GNOME's top bar)", () => {
  const pos = anchorPosition(input({ primary: panelled("top") }));
  assert.deepEqual(pos, { x: 1920 - 640, y: 48 });
});

test("hugs a left panel on Linux", () => {
  const pos = anchorPosition(input({ primary: panelled("left") }));
  assert.deepEqual(pos, { x: 48, y: 1080 - 440 });
});

test("hugs a right panel on Linux", () => {
  const pos = anchorPosition(input({ primary: panelled("right") }));
  assert.deepEqual(pos, { x: 1872 - 640, y: 1080 - 440 });
});

test("centres under the tray icon on Linux when its bounds are known", () => {
  const pos = anchorPosition(
    input({ primary: panelled("bottom"), trayBounds: { x: 900, y: 1032, width: 24, height: 48 } })
  );
  assert.deepEqual(pos, { x: 912 - 320, y: 1032 - 440 });
});

test("keeps a tray-centred window inside the work area", () => {
  const pos = anchorPosition(
    input({ primary: panelled("bottom"), trayBounds: { x: 1900, y: 1032, width: 20, height: 48 } })
  );
  assert.deepEqual(pos, { x: 1920 - 640, y: 1032 - 440 });
});

test("ignores zero-sized tray bounds from AppIndicator trays", () => {
  const pos = anchorPosition(
    input({ primary: panelled("bottom"), trayBounds: { x: 0, y: 0, width: 0, height: 0 } })
  );
  assert.deepEqual(pos, { x: 1920 - 640, y: 1032 - 440 });
});

test("keeps the macOS top-right corner it already used", () => {
  const mac = display({ x: 0, y: 0, width: 1440, height: 900 }, { x: 0, y: 25, width: 1440, height: 875 });
  const pos = anchorPosition(
    input({ primary: mac, platform: "darwin", trayBounds: { x: 700, y: 0, width: 22, height: 24 } })
  );
  assert.deepEqual(pos, { x: 1440 - 640 - 8, y: 25 + 8 });
});

test("keeps the Windows bottom-right corner it already used", () => {
  const win = display({ x: 0, y: 0, width: 1920, height: 1080 }, { x: 0, y: 0, width: 1920, height: 1040 });
  const pos = anchorPosition(
    input({ primary: win, platform: "win32", trayBounds: { x: 1700, y: 1040, width: 24, height: 40 } })
  );
  assert.deepEqual(pos, { x: 1920 - 640, y: 1040 - 440 });
});

test("anchors on the display holding the tray icon, not the primary one", () => {
  const primary = panelled("bottom");
  const secondary = panelled("top", 1920);
  const pos = anchorPosition(
    input({
      primary,
      displays: [primary, secondary],
      trayBounds: { x: 3000, y: 0, width: 24, height: 48 },
      cursor: { x: 10, y: 10 }
    })
  );
  assert.deepEqual(pos, { x: 3000 + 12 - 320, y: 48 });
});

test("falls back to the pointer's display when tray bounds are unknown", () => {
  const primary = panelled("bottom");
  const secondary = panelled("bottom", 1920);
  const chosen = pickDisplay(
    input({ primary, displays: [primary, secondary], cursor: { x: 2500, y: 1050 } })
  );
  assert.equal(chosen, secondary);
});

test("falls back to the primary display when the pointer is off-screen", () => {
  const primary = panelled("bottom");
  const chosen = pickDisplay(input({ primary, cursor: { x: 9999, y: 9999 } }));
  assert.equal(chosen, primary);
});

test("survives a work area smaller than the window", () => {
  const tiny = display({ x: 0, y: 0, width: 400, height: 300 }, { x: 0, y: 0, width: 400, height: 280 });
  const pos = anchorPosition(input({ primary: tiny }));
  assert.deepEqual(pos, { x: 0, y: 0 });
});

test("samePoint tolerates a one-pixel drift but not more", () => {
  assert.equal(samePoint({ x: 10, y: 10 }, { x: 11, y: 9 }), true);
  assert.equal(samePoint({ x: 10, y: 10 }, { x: 13, y: 10 }), false);
});
