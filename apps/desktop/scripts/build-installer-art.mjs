// Generates the NSIS installer BMP artwork from the committed source PNGs.
//
// NSIS/MUI requires 24-bit BMP images at exact pixel sizes for the assisted
// installer's welcome/finish sidebar (164x314) and inner-page header (150x57).
// This script downscales the high-res source PNGs in build/art-src to those
// exact dimensions, flattening onto the brand background (no transparency),
// and writes 24-bit BMPs into build/.
//
// The generated BMPs are committed, so CI does not need to run this — it is a
// dev-time asset step. Rendering uses PowerShell's System.Drawing and therefore
// only runs on Windows; on other platforms it no-ops and the committed BMPs are
// used as-is. If a source PNG is missing it falls back to compositing the app
// logo (build/PangeaVPN.png).
//
// Usage: node ./scripts/build-installer-art.mjs

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const desktopDir = path.resolve(scriptDir, "..");
const buildDir = path.join(desktopDir, "build");
const srcDir = path.join(buildDir, "art-src");
const logoFallback = path.join(buildDir, "PangeaVPN.png");

// Brand background (warm near-black #141312) used to flatten transparency.
const BG_HEX = "141312";

const targets = [
  {
    src: path.join(srcDir, "installer-sidebar.png"),
    out: path.join(buildDir, "installerSidebar.bmp"),
    width: 164,
    height: 314
  },
  {
    src: path.join(srcDir, "installer-header.png"),
    out: path.join(buildDir, "installerHeader.bmp"),
    width: 150,
    height: 57
  }
];

if (process.platform !== "win32") {
  console.log(
    "build-installer-art: non-Windows platform; skipping BMP generation " +
      "(committed BMPs are used)."
  );
  process.exit(0);
}

// PowerShell converter. Kept as single-quoted JS strings so PowerShell '$'
// variables are not treated as JS template interpolation.
const ps = [
  "param(",
  "  [Parameter(Mandatory=$true)][string]$Src,",
  "  [Parameter(Mandatory=$true)][string]$Out,",
  "  [Parameter(Mandatory=$true)][int]$Width,",
  "  [Parameter(Mandatory=$true)][int]$Height,",
  "  [string]$Fallback = '',",
  '  [string]$BgHex = "141312"',
  ")",
  "$ErrorActionPreference = 'Stop'",
  "Add-Type -AssemblyName System.Drawing",
  "",
  "$source = $Src",
  "if (-not (Test-Path $source)) {",
  "  if ($Fallback -ne '' -and (Test-Path $Fallback)) { $source = $Fallback }",
  "  else { throw \"No source PNG and no fallback for $Out\" }",
  "}",
  "",
  "$r = [Convert]::ToInt32($BgHex.Substring(0,2),16)",
  "$g = [Convert]::ToInt32($BgHex.Substring(2,2),16)",
  "$b = [Convert]::ToInt32($BgHex.Substring(4,2),16)",
  "$bg = [System.Drawing.Color]::FromArgb(255,$r,$g,$b)",
  "",
  "$img = [System.Drawing.Image]::FromFile($source)",
  "try {",
  "  $bmp = New-Object System.Drawing.Bitmap($Width,$Height,[System.Drawing.Imaging.PixelFormat]::Format24bppRgb)",
  "  try {",
  "    $gfx = [System.Drawing.Graphics]::FromImage($bmp)",
  "    try {",
  "      $gfx.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic",
  "      $gfx.PixelOffsetMode   = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality",
  "      $gfx.SmoothingMode     = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality",
  "      $gfx.Clear($bg)",
  "      $scale = [math]::Min($Width / $img.Width, $Height / $img.Height)",
  "      $dw = [int][math]::Round($img.Width * $scale)",
  "      $dh = [int][math]::Round($img.Height * $scale)",
  "      $dx = [int][math]::Round(($Width - $dw) / 2)",
  "      $dy = [int][math]::Round(($Height - $dh) / 2)",
  "      $rect = New-Object System.Drawing.Rectangle($dx,$dy,$dw,$dh)",
  "      $gfx.DrawImage($img,$rect,0,0,$img.Width,$img.Height,[System.Drawing.GraphicsUnit]::Pixel)",
  "    } finally { $gfx.Dispose() }",
  "    $bmp.Save($Out,[System.Drawing.Imaging.ImageFormat]::Bmp)",
  "  } finally { $bmp.Dispose() }",
  "} finally { $img.Dispose() }"
].join("\n");

// Private, unpredictable temp dir — a fixed name in the shared temp dir lets a
// local attacker pre-create the file and get their own script run instead.
const ps1Dir = fs.mkdtempSync(path.join(os.tmpdir(), "pangea-installer-art-"));
const ps1Path = path.join(ps1Dir, "render.ps1");
fs.writeFileSync(ps1Path, ps, "utf8");

try {
  for (const t of targets) {
    execFileSync(
      "powershell.exe",
      [
        "-NoProfile",
        "-ExecutionPolicy",
        "Bypass",
        "-File",
        ps1Path,
        "-Src",
        t.src,
        "-Out",
        t.out,
        "-Width",
        String(t.width),
        "-Height",
        String(t.height),
        "-Fallback",
        logoFallback,
        "-BgHex",
        BG_HEX
      ],
      { stdio: "inherit" }
    );
    console.log(
      `Generated ${path.relative(desktopDir, t.out)} (${t.width}x${t.height})`
    );
  }
} finally {
  fs.rmSync(ps1Dir, { recursive: true, force: true });
}
