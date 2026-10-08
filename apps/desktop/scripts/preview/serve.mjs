// Serves the built renderer to a plain browser, with mock-bridge.js standing in for Electron's
// preload so UI work can be seen without the daemon or the hub. Never part of a packaged build.
import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import http from "node:http";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const packageDir = path.resolve(scriptDir, "..", "..");
const distDir = path.join(packageDir, "dist");
const mockFile = path.join(scriptDir, "mock-bridge.js");
const tscBin = path.resolve(packageDir, "..", "..", "node_modules", "typescript", "bin", "tsc");

const args = process.argv.slice(2);
const watch = args.includes("--watch");
const portArg = args.find((a) => a.startsWith("--port="));
const port = Number(portArg ? portArg.slice("--port=".length) : process.env.PORT || 5199);

const types = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".png": "image/png",
  ".svg": "image/svg+xml",
  ".json": "application/json"
};

function copyStatic() {
  const result = spawnSync(process.execPath, ["scripts/copy-static.mjs"], { cwd: packageDir, stdio: "inherit" });
  if (result.status !== 0) process.exit(result.status ?? 1);
}

function buildOnce() {
  const result = spawnSync(process.execPath, [tscBin, "-p", "tsconfig.renderer.json"], { cwd: packageDir, stdio: "inherit" });
  if (result.status !== 0) process.exit(result.status ?? 1);
  copyStatic();
}

function startWatching() {
  spawn(process.execPath, [tscBin, "-p", "tsconfig.renderer.json", "--watch", "--preserveWatchOutput"], {
    cwd: packageDir,
    stdio: "inherit"
  });
  let pending = null;
  for (const file of ["src/renderer/index.html", "src/renderer/styles.css"]) {
    fs.watch(path.join(packageDir, file), () => {
      clearTimeout(pending);
      pending = setTimeout(copyStatic, 50);
    });
  }
}

function send(res, status, type, body) {
  res.writeHead(status, { "content-type": type, "cache-control": "no-store" });
  res.end(body);
}

buildOnce();
if (watch) startWatching();

http
  .createServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://preview");
    if (url.pathname === "/") {
      res.writeHead(302, { location: `/renderer/index.html${url.search}` });
      return res.end();
    }
    if (url.pathname === "/__mock-bridge.js") return send(res, 200, types[".js"], fs.readFileSync(mockFile));

    const file = path.resolve(distDir, `.${decodeURIComponent(url.pathname)}`);
    if (!file.startsWith(distDir + path.sep) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) {
      return send(res, 404, "text/plain", "not found");
    }
    let body = fs.readFileSync(file);
    if (path.basename(file) === "index.html") {
      // Ahead of the module script, so the bridge exists before the renderer looks for it.
      body = body.toString().replace('<script type="module"', '<script src="/__mock-bridge.js"></script>\n    <script type="module"');
    }
    send(res, 200, types[path.extname(file)] ?? "application/octet-stream", body);
  })
  .listen(port, "127.0.0.1", () => {
    console.log(`\nUI preview: http://127.0.0.1:${port}/renderer/index.html`);
    console.log("Query options: sub=active|expired|none  state=DISCONNECTED|CONNECTED|CONNECTING|ERROR  ks=1");
    console.log("               auth=0  lang=en|es|fr|ru|uk|zh|ar|fa  platform=darwin|win32|linux  latency=<ms>");
    console.log(watch ? "Watching for changes; reload the page to see them.\n" : "Run with --watch to rebuild on change.\n");
  });
