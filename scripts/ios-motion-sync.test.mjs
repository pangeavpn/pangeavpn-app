import assert from "node:assert/strict";
import test from "node:test";
import { checkTrack, geometry, measureFrame } from "./ios-motion-sync.mjs";

const g = geometry(1206);
const WIDTH = g.cropWidth;
const HEIGHT = 400;
const PAGE = [251, 249, 242];
const AMBER = [224, 150, 40];
const GREEN = [70, 170, 120];

function blankFrame() {
  const rgb = Buffer.alloc(WIDTH * HEIGHT * 3);
  for (let i = 0; i < WIDTH * HEIGHT; i++) rgb.set(PAGE, i * 3);
  return rgb;
}

function fill(rgb, colour, inside) {
  for (let y = 0; y < HEIGHT; y++) {
    for (let x = 0; x < WIDTH; x++) if (inside(x, y)) rgb.set(colour, (y * WIDTH + x) * 3);
  }
}

function row({ label, dot, labelColour = AMBER, dotColour = labelColour }) {
  const rgb = blankFrame();
  fill(rgb, labelColour, (x, y) => x >= 123 && x < 380 && Math.abs(y - label) <= 12);
  fill(rgb, dotColour, (x, y) => (x - 72) ** 2 + (y - dot) ** 2 <= 12 ** 2);
  return rgb;
}

const track = (rows) => rows.map(([label, dot], frame) => ({ frame, label, dot }));

test("measureFrame finds the dot and label centres", () => {
  const found = measureFrame(row({ label: 200, dot: 203 }), WIDTH, HEIGHT, g);
  assert.ok(found);
  assert.ok(Math.abs(found.label - 200) < 0.5);
  assert.ok(Math.abs(found.dot - 203) < 0.5);
});

test("measureFrame ignores a flag and a row border that are not the status row", () => {
  const rgb = blankFrame();
  fill(rgb, [200, 40, 50], (x, y) => x >= 150 && x < 210 && y >= 180 && y < 222);
  fill(rgb, [195, 86, 43], (x, y) => x >= 58 && x < 61 && y >= 150 && y < 260);
  assert.equal(measureFrame(rgb, WIDTH, HEIGHT, g), null);
});

test("measureFrame needs the dot and label in the same colour", () => {
  assert.equal(measureFrame(row({ label: 200, dot: 200, dotColour: GREEN }), WIDTH, HEIGHT, g), null);
});

test("checkTrack passes a row whose dot and label slide together", () => {
  const slide = [150, 160, 172, 185, 194, 199, 200, ...Array(10).fill(200)];
  const result = checkTrack(track(slide.map((y) => [y, y + 1])), g);
  assert.equal(result.ok, true);
  assert.ok(result.moving >= 5);
});

test("checkTrack keeps a fixed design offset between dot and label", () => {
  const slide = [150, 170, 190, 200, ...Array(10).fill(200)];
  assert.equal(checkTrack(track(slide.map((y) => [y, y + 3])), g).ok, true);
});

test("checkTrack fails when the dot runs ahead of its label mid-slide", () => {
  const rows = [[150, 180], [160, 192], [172, 198], [185, 200], [194, 200], [199, 200], ...Array(10).fill([200, 200])];
  const result = checkTrack(track(rows), g);
  assert.equal(result.ok, false);
  assert.equal(result.worst.frame, 1);
  assert.equal(Math.round(result.worst.deviation), 32);
});

test("checkTrack refuses a tour where the row never moves", () => {
  const result = checkTrack(track(Array(20).fill([200, 200])), g);
  assert.equal(result.exercised, false);
  assert.equal(result.ok, false);
});
