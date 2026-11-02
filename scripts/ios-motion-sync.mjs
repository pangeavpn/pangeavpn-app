// Checks the recorded motion tour: the status dot must slide with its label on every frame.
// Usage: node scripts/ios-motion-sync.mjs <tour.mp4>
import { spawn } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const SATURATION = 60;
const REFERENCE_WIDTH = 1206;
const SETTLED_RUN = 8;
const STILL = 0.75;

// Pixel bounds for a 1206 px wide (iPhone 17 Pro) capture, scaled to the video's width.
export function geometry(width) {
  const s = width / REFERENCE_WIDTH;
  const px = (v) => Math.round(v * s);
  return {
    cropWidth: Math.min(width, px(420)),
    dot: { x0: px(40), x1: px(108), min: px(15), max: px(36) },
    label: { x0: px(118), x1: px(390), minHeight: px(14), maxHeight: px(50), minInk: px(120), minWidth: px(150) },
    reach: px(60),
    tolerance: 4 * s,
    minMoving: 1,
  };
}

function bandRows(rgb, width, height, x0, x1, y0 = 0, y1 = height) {
  const rows = [];
  for (let y = y0; y < y1; y++) {
    let count = 0, minX = Infinity, maxX = -1, r = 0, g = 0, b = 0;
    for (let x = x0; x < x1; x++) {
      const i = (y * width + x) * 3;
      const pr = rgb[i], pg = rgb[i + 1], pb = rgb[i + 2];
      if (Math.max(pr, pg, pb) - Math.min(pr, pg, pb) <= SATURATION) continue;
      count++; r += pr; g += pg; b += pb;
      if (x < minX) minX = x;
      if (x > maxX) maxX = x;
    }
    rows.push({ y, count, minX, maxX, r, g, b });
  }
  return rows;
}

function groups(rows, minCount, gap) {
  const out = [];
  let current = null;
  for (const row of rows) {
    if (row.count < minCount) continue;
    if (current && row.y - current.at(-1).y <= gap) current.push(row);
    else out.push((current = [row]));
  }
  return out;
}

function summarise(group) {
  const ink = group.reduce((sum, row) => sum + row.count, 0);
  const centre = group.reduce((sum, row) => sum + row.y * row.count, 0) / ink;
  const left = Math.min(...group.map((row) => row.minX));
  const right = Math.max(...group.map((row) => row.maxX));
  const colour = ["r", "g", "b"].map((k) => group.reduce((sum, row) => sum + row[k], 0) / ink);
  return { ink, centre, height: group.at(-1).y - group[0].y + 1, width: right - left + 1, colour };
}

const distance = (a, b) => Math.hypot(a[0] - b[0], a[1] - b[1], a[2] - b[2]);

// Finds the kicker as a wide saturated text line with a round dot of the same colour to its left.
export function measureFrame(rgb, width, height, g = geometry(width)) {
  const { label, dot } = g;
  const lines = groups(bandRows(rgb, width, height, label.x0, label.x1), 3, 3).map(summarise)
    .filter((l) => l.height >= label.minHeight && l.height <= label.maxHeight)
    .filter((l) => l.ink >= label.minInk && l.width >= label.minWidth);
  for (const line of lines) {
    const y0 = Math.max(0, Math.floor(line.centre - g.reach));
    const y1 = Math.min(height, Math.ceil(line.centre + g.reach));
    const blobs = groups(bandRows(rgb, width, height, dot.x0, dot.x1, y0, y1), 2, 2).map(summarise)
      .filter((b) => b.height >= dot.min && b.height <= dot.max && b.width >= dot.min && b.width <= dot.max)
      .filter((b) => b.ink / (b.width * b.height) >= 0.5 && distance(b.colour, line.colour) <= 80);
    if (blobs.length === 0) continue;
    const nearest = blobs.reduce((a, b) => (Math.abs(a.centre - line.centre) <= Math.abs(b.centre - line.centre) ? a : b));
    return { label: line.centre, dot: nearest.centre };
  }
  return null;
}

// Each sample's dot offset is judged against the offset the row settles at, so a fixed design offset passes.
export function checkTrack(samples, g) {
  const settledFrom = new Array(samples.length);
  let settled = samples.length ? samples.at(-1).dot - samples.at(-1).label : 0;
  for (let i = samples.length - 1; i >= 0; i--) {
    const run = samples.slice(i, i + SETTLED_RUN);
    if (run.length === SETTLED_RUN && run.every((s) => Math.abs(s.label - samples[i].label) < STILL)) {
      settled = samples[i].dot - samples[i].label;
    }
    settledFrom[i] = settled;
  }
  let moving = 0;
  const judged = samples.map((s, i) => {
    if (i > 0 && Math.abs(s.label - samples[i - 1].label) >= STILL) moving++;
    return { frame: s.frame, deviation: Math.abs(s.dot - s.label - settledFrom[i]) };
  });
  const ranked = [...judged].sort((a, b) => b.deviation - a.deviation);
  const worst = ranked[0] ?? { deviation: 0, frame: null };
  const labels = samples.map((s) => s.label);
  const slide = labels.length ? Math.max(...labels) - Math.min(...labels) : 0;
  const exercised = moving >= g.minMoving;
  const ok = exercised && worst.deviation <= g.tolerance;
  return { ok, exercised, worst, worstFrames: ranked.slice(0, 5), moving, slide, samples: samples.length };
}

function probeWidth(video) {
  return new Promise((resolve, reject) => {
    const args = ["-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "csv=p=0", video];
    const probe = spawn("ffprobe", args);
    let out = "";
    probe.stdout.on("data", (d) => (out += d));
    probe.on("error", reject);
    probe.on("close", (code) => {
      const [w, h] = out.trim().split(",").map(Number);
      code === 0 && w > 0 && h > 0 ? resolve({ width: w, height: h }) : reject(new Error(`ffprobe failed on ${video}`));
    });
  });
}

async function measureVideo(video) {
  const { width, height } = await probeWidth(video);
  const g = geometry(width);
  const args = ["-nostdin", "-v", "error", "-i", video, "-fps_mode", "passthrough",
    "-vf", `crop=${g.cropWidth}:${height}:0:0`, "-f", "rawvideo", "-pix_fmt", "rgb24", "-"];
  const ffmpeg = spawn("ffmpeg", args, { stdio: ["ignore", "pipe", "pipe"] });
  // The recorder stamps some frames twice; every frame is still wanted, so only that warning is dropped.
  let partial = "";
  ffmpeg.stderr.on("data", (d) => {
    const lines = (partial + d.toString()).split("\n");
    partial = lines.pop();
    const kept = lines.filter((l) => l.trim() && !/non monotonically|Last message repeated/.test(l));
    if (kept.length) process.stderr.write(`${kept.join("\n")}\n`);
  });
  const size = g.cropWidth * height * 3;
  const samples = [];
  let pending = Buffer.alloc(0), frame = 0;
  for await (const chunk of ffmpeg.stdout) {
    pending = Buffer.concat([pending, chunk]);
    while (pending.length >= size) {
      const found = measureFrame(pending.subarray(0, size), g.cropWidth, height, g);
      if (found) samples.push({ frame, ...found });
      pending = pending.subarray(size);
      frame++;
    }
  }
  const code = await new Promise((resolve) => (ffmpeg.exitCode !== null ? resolve(ffmpeg.exitCode) : ffmpeg.on("close", resolve)));
  if (code !== 0) throw new Error(`ffmpeg exited ${code} on ${video}`);
  return { g, samples, frames: frame };
}

async function main() {
  const video = process.argv[2];
  if (!video) {
    console.error("usage: node scripts/ios-motion-sync.mjs <tour.mp4>");
    process.exit(2);
  }
  const { g, samples, frames } = await measureVideo(video);
  const result = checkTrack(samples, g);
  const worst = result.worst.deviation.toFixed(1);
  console.log(`${frames} frames, status row found in ${result.samples}, ${result.moving} mid-slide, slid ${result.slide.toFixed(0)}px`);
  console.log(`widest dot/label split: ${worst}px at frame ${result.worst.frame} (allowed ${g.tolerance.toFixed(1)}px)`);
  console.log(`worst frames: ${result.worstFrames.map((w) => `${w.frame}=${w.deviation.toFixed(1)}px`).join(", ")}`);
  if (!result.exercised) {
    console.error("::error::the status row was never caught sliding, so the tour proves nothing");
    process.exit(1);
  }
  if (!result.ok) {
    console.error(`::error::the status dot and its label came apart by ${worst}px at frame ${result.worst.frame}`);
    process.exit(1);
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    console.error(`::error::${error.message}`);
    process.exit(1);
  });
}
