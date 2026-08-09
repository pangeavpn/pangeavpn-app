import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { ensurePangeaNaiveLib } from "./naive-download.mjs";

// The mac frameworks the pangea_naive archive's objects reference, read off
// the is_apple/is_mac `frameworks` declarations of the GN targets bundled
// into the complete_static_lib (base, net, crypto, partition_alloc) — the
// darwin analog of WINDOWS_SYSTEM_LIBS in naive-cgo.mjs.
const MAC_FRAMEWORKS = [
  "AppKit", "ApplicationServices", "CFNetwork", "CoreFoundation",
  "CoreGraphics", "CoreServices", "CoreText", "CryptoTokenKit", "Foundation",
  "IOKit", "LocalAuthentication", "Network", "OpenCL", "OpenDirectory",
  "Security", "SystemConfiguration", "UniformTypeIdentifiers"
];

// System libraries the archive's objects need beyond the frameworks: net's
// is_mac block links libresolv, and base's Mach port-rendezvous code
// (audit_token_to_pid) links libbsm.
const MAC_LIBS = ["-lresolv", "-lbsm"];

// Matches mac_deployment_target in the fork's build/config/mac/mac_sdk.gni.
const MAC_DEPLOYMENT_TARGET = "12.0";

const CLANG_ARCH = { amd64: "x86_64", arm64: "arm64" };
const LOCAL_OUT_DIR = { amd64: "pangea-mac-x64", arm64: "pangea-mac-arm64" };

// Darwin counterpart of the Windows resolver in naive-cgo.mjs: resolves the
// lib + header (a local fork build tree if present, else the pinned
// prebuilt) and returns the cgo build config, or null (after logging why)
// so callers fall back to the stub transport. Unlike Windows this needs no
// CC wrapper or MSVC env capture — Apple clang is cgo's default CC; the
// -arch flags make the x64 cross-build from the arm64 CI host work.
export function resolveNaiveCgoConfigDarwin(goArch, rootDir) {
  const clangArch = CLANG_ARCH[goArch];
  const localOutDir = LOCAL_OUT_DIR[goArch];
  if (!clangArch || !localOutDir) {
    return null;
  }

  const naiveproxySrc =
    process.env.PANGEA_NAIVEPROXY_SRC || path.join(rootDir, "..", "naiveproxy", "src");

  let libDir = path.join(naiveproxySrc, "out", localOutDir, "obj", "net");
  let headerDir = path.join(naiveproxySrc, "pangea", "capi");
  const libName = "libpangea_naive.a";
  const headerName = "pangea_naive_capi.h";
  if (!fs.existsSync(path.join(libDir, libName)) || !fs.existsSync(path.join(headerDir, headerName))) {
    const prebuilt = ensurePangeaNaiveLib(goArch, rootDir);
    if (!prebuilt) {
      console.warn(
        `naive_cgo: no local build at ${libDir} and no pinned prebuilt available; ` +
          `naive transport falls back to the stub for ${goArch}.`
      );
      return null;
    }
    libDir = prebuilt.libDir;
    headerDir = prebuilt.headerDir;
  }

  const archFlags = `-arch ${clangArch} -mmacosx-version-min=${MAC_DEPLOYMENT_TARGET}`;
  return {
    tags: ["naive_cgo"],
    env: {
      CGO_ENABLED: "1",
      CGO_CFLAGS: `-I${headerDir} ${archFlags}`,
      CGO_LDFLAGS: [
        `-L${libDir}`,
        "-lpangea_naive",
        ...MAC_LIBS,
        ...MAC_FRAMEWORKS.map((name) => `-framework ${name}`),
        archFlags
      ].join(" ")
    }
  };
}
