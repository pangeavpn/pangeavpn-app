import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { resolveNaiveCgoConfigDarwin } from "./naive-cgo-darwin.mjs";
import { ensurePangeaNaiveLib } from "./naive-download.mjs";

// Chromium's default_libs plus pangea_naive's transitive libs, read off a real
// GN link line — a static lib can't carry its consumer's required libs.
const WINDOWS_SYSTEM_LIBS = [
  "-ladvapi32", "-lcomdlg32", "-ldbghelp", "-ldnsapi", "-lgdi32", "-lmsimg32",
  "-lodbc32", "-lodbccp32", "-loleaut32", "-lshell32", "-lshlwapi", "-luser32",
  "-lusp10", "-luuid", "-lversion", "-lwininet", "-lwinmm", "-lwinspool",
  "-lws2_32", "-ldelayimp", "-lole32", "-lcrypt32", "-ldhcpcsvc", "-liphlpapi",
  "-lncrypt", "-lrpcrt4", "-lsecur32", "-lurlmon", "-lwinhttp", "-lcfgmgr32",
  "-lntdll", "-lonecore", "-lpdh", "-lpowrprof", "-lpropsys", "-lsetupapi",
  "-lshcore", "-ltbs", "-luserenv", "-lwbemuuid", "-lbcrypt"
];

const GN_ARCH_DIR = { amd64: "pangea-x64", arm64: "pangea-arm64" };
const CLANG_TARGET = { amd64: "x86_64-pc-windows-msvc", arm64: "aarch64-pc-windows-msvc" };
const VCVARS_ARCH = { amd64: "x64", arm64: "arm64" };

// Returns null when the lib or toolchain is missing so callers fall back to
// the stub; PANGEA_REQUIRE_NAIVE turns that silent fallback into a failure.
export function resolveNaiveCgoConfig(goArch, rootDir) {
  const config = resolveConfig(goArch, rootDir);
  if (!config && naiveIsRequired()) {
    throw new Error(
      `naive_cgo did not resolve for ${goArch} and PANGEA_REQUIRE_NAIVE is set; ` +
        `see the naive_cgo warning above for the reason. Refusing to build the stub transport.`
    );
  }
  return config;
}

function naiveIsRequired() {
  const raw = String(process.env.PANGEA_REQUIRE_NAIVE ?? "").trim().toLowerCase();
  return raw !== "" && raw !== "0" && raw !== "false";
}

function resolveConfig(goArch, rootDir) {
  if (process.platform === "darwin") {
    return resolveNaiveCgoConfigDarwin(goArch, rootDir);
  }
  if (process.platform !== "win32") {
    return null;
  }

  const clangTarget = CLANG_TARGET[goArch];
  const gnArchDir = GN_ARCH_DIR[goArch];
  const vcvarsArch = VCVARS_ARCH[goArch];
  if (!clangTarget || !gnArchDir) {
    return null;
  }

  const naiveproxySrc =
    process.env.PANGEA_NAIVEPROXY_SRC || path.join(rootDir, "..", "naiveproxy", "src");

  // A local Chromium build tree if present, else the pinned prebuilt. The
  // prebuilt is native COFF, so it links against any recent clang-cl.
  let libPath = path.join(naiveproxySrc, "out", gnArchDir, "obj", "net", "pangea_naive.lib");
  let headerDir = path.join(naiveproxySrc, "pangea", "capi");
  const headerPath = path.join(headerDir, "pangea_naive_capi.h");
  if (!fs.existsSync(libPath) || !fs.existsSync(headerPath)) {
    const prebuilt = ensurePangeaNaiveLib(goArch, rootDir);
    if (!prebuilt) {
      console.warn(
        `naive_cgo: no local build at ${libPath} and no pinned prebuilt available; ` +
          `naive transport falls back to the stub for ${goArch}.`
      );
      return null;
    }
    libPath = path.join(prebuilt.libDir, prebuilt.libName);
    headerDir = prebuilt.headerDir;
  }

  // Pinned Chromium clang from a local checkout if present, else system
  // clang-cl (preinstalled on CI runners); the COFF lib links against either.
  const clangPath = resolveClang(naiveproxySrc);
  if (!clangPath) {
    console.warn(
      `naive_cgo: no clang-cl found (pinned toolchain or system LLVM); naive transport falls back to the stub for ${goArch}.`
    );
    return null;
  }

  const vcvars = findVcvarsEnv(vcvarsArch);
  if (!vcvars) {
    console.warn(
      "naive_cgo: could not locate a Visual Studio installation (vcvarsall.bat); " +
        `naive transport falls back to the stub for ${goArch}.`
    );
    return null;
  }

  const wrapperPath = buildCcWrapper(rootDir);
  if (!wrapperPath) {
    console.warn(`naive_cgo: failed to build the CC wrapper; naive transport falls back to the stub for ${goArch}.`);
    return null;
  }

  return {
    tags: ["naive_cgo"],
    env: {
      CGO_ENABLED: "1",
      CC: wrapperPath,
      CGO_CFLAGS: `-I${toForwardSlash(headerDir)}`,
      CGO_LDFLAGS: [
        `-L${toForwardSlash(path.dirname(libPath))}`,
        "-lpangea_naive",
        ...WINDOWS_SYSTEM_LIBS,
        "-Wl,/DEFAULTLIB:libcpmt.lib"
      ].join(" "),
      NAIVE_CC_REAL_CC: clangPath,
      NAIVE_CC_TARGET: clangTarget,
      INCLUDE: vcvars.INCLUDE,
      LIB: vcvars.LIB
    }
  };
}

function toForwardSlash(p) {
  return p.replaceAll("\\", "/");
}

// Finds the clang.exe cgo invokes: pinned Chromium toolchain first, else a
// system clang-cl via PANGEA_CLANG_CL, the LLVM install dir, or PATH.
function resolveClang(naiveproxySrc) {
  const pinnedBin = path.join(naiveproxySrc, "third_party", "llvm-build", "Release+Asserts", "bin");
  const candidates = [
    path.join(pinnedBin, "clang-cl.exe"),
    process.env.PANGEA_CLANG_CL,
    path.join(process.env.ProgramFiles ?? "C:\\Program Files", "LLVM", "bin", "clang-cl.exe"),
    whichExe("clang-cl.exe")
  ];
  for (const clangCl of candidates) {
    if (clangCl && fs.existsSync(clangCl)) {
      return ensureClangDriver(path.dirname(clangCl), clangCl);
    }
  }
  return null;
}

// Same binary, but argv[0] picks the driver and cgo invokes it as clang.exe,
// so hand back a sibling clang.exe, copying one when it's absent.
function ensureClangDriver(binDir, clangClPath) {
  const clangPath = path.join(binDir, "clang.exe");
  if (!fs.existsSync(clangPath)) {
    fs.copyFileSync(clangClPath, clangPath);
  }
  return clangPath;
}

// Minimal PATH lookup for an executable; returns its full path or null.
function whichExe(exe) {
  const result = spawnSync("where", [exe], { encoding: "utf8", shell: false });
  if (result.error || (result.status ?? 1) !== 0) {
    return null;
  }
  return result.stdout.split(/\r?\n/).map((s) => s.trim()).find(Boolean) || null;
}

// Builds (or reuses) naive-cc-wrapper.exe; it must be a real executable
// because Go's os/exec cannot invoke a .bat/.cmd as CC.
function buildCcWrapper(rootDir) {
  const wrapperDir = path.join(rootDir, "scripts", "naive-cc-wrapper");
  const cacheDir = path.join(rootDir, ".cache", "naive-cc-wrapper");
  const outPath = path.join(cacheDir, "naive-cc-wrapper.exe");

  if (fs.existsSync(outPath)) {
    return outPath;
  }

  fs.mkdirSync(cacheDir, { recursive: true });
  const result = spawnSync("go", ["build", "-o", outPath, "."], {
    cwd: wrapperDir,
    stdio: "inherit",
    shell: false,
    env: { ...process.env, CGO_ENABLED: "0" }
  });

  if (result.error || (result.status ?? 1) !== 0) {
    return null;
  }
  return outPath;
}

// Runs vcvarsall.bat to capture INCLUDE/LIB for the arch, so no prior
// interactive setup is needed.
function findVcvarsEnv(arch) {
  const vcvarsallPath = findVcvarsall();
  if (!vcvarsallPath) {
    return null;
  }

  const result = spawnSync(`"${vcvarsallPath}" ${arch} && set`, {
    shell: true,
    encoding: "utf8"
  });
  if (result.error || (result.status ?? 1) !== 0) {
    return null;
  }

  const env = {};
  for (const line of result.stdout.split(/\r?\n/)) {
    const eq = line.indexOf("=");
    if (eq <= 0) continue;
    const key = line.slice(0, eq).toUpperCase();
    if (key === "INCLUDE" || key === "LIB") {
      env[key] = line.slice(eq + 1);
    }
  }
  if (!env.INCLUDE || !env.LIB) {
    return null;
  }
  return env;
}

// vswhere finds any VS version/edition (VS 2026 roots are a major version,
// not a year), with a static probe of known install dirs as fallback.
function findVcvarsall() {
  const pf86 = process.env["ProgramFiles(x86)"] ?? "C:\\Program Files (x86)";
  const vswhere = path.join(pf86, "Microsoft Visual Studio", "Installer", "vswhere.exe");
  if (fs.existsSync(vswhere)) {
    const result = spawnSync(
      vswhere,
      ["-latest", "-products", "*", "-find", "VC\\Auxiliary\\Build\\vcvarsall.bat"],
      { encoding: "utf8", shell: false }
    );
    if (!result.error && (result.status ?? 1) === 0) {
      const found = result.stdout.split(/\r?\n/).map((s) => s.trim()).find(Boolean);
      if (found && fs.existsSync(found)) {
        return found;
      }
    }
  }

  const roots = [pf86, process.env["ProgramFiles"] ?? "C:\\Program Files"];
  const versions = ["2022", "18"];
  const editions = ["BuildTools", "Community", "Professional", "Enterprise", "Insiders"];
  for (const root of roots) {
    for (const version of versions) {
      for (const edition of editions) {
        const candidate = path.join(
          root,
          "Microsoft Visual Studio",
          version,
          edition,
          "VC",
          "Auxiliary",
          "Build",
          "vcvarsall.bat"
        );
        if (fs.existsSync(candidate)) {
          return candidate;
        }
      }
    }
  }
  return null;
}
