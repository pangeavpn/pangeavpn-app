import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { resolveNaiveCgoConfigDarwin } from "./naive-cgo-darwin.mjs";
import { ensurePangeaNaiveLib } from "./naive-download.mjs";

// The Windows default_libs list Chromium executables get automatically
// (build/config/BUILD.gn:config("default_libs")) plus the pangea_naive
// target's own transitive libs, read off a real GN executable link line.
// See the naiveproxy transport design's
// "Known risk" section for how this list was derived and why it can't be
// discovered from pangea_naive.lib itself (static libraries don't carry
// their consumer's required libs the way GN's complete_static_lib bundles
// object code).
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

// Resolves the naive_cgo build config for the given Go arch ("amd64" |
// "arm64") on the current platform (Windows here, darwin via
// naive-cgo-darwin.mjs). Returns null (after logging why) if the naiveproxy
// fork's build artifacts or a usable toolchain aren't available on this
// machine — callers should fall back to building without the naive_cgo tag
// (the stub transport) rather than failing the build.
export function resolveNaiveCgoConfig(goArch, rootDir) {
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

  // Resolve the lib + header: a local Chromium build tree if present, else the
  // pinned prebuilt fetched from the fork's release. The prebuilt is native
  // COFF (built with use_thin_lto=false), so it links against any recent
  // clang-cl — no longer only the exact pinned Chromium toolchain.
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

  // Resolve a clang for cgo's compile+link: the pinned Chromium clang from a
  // local checkout if present, else a system clang-cl (LLVM is preinstalled on
  // CI runners). Native-COFF pangea_naive.lib links against either.
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

// Resolves a clang.exe (the GCC-style driver name cgo invokes) for the CC
// wrapper. Prefers the pinned Chromium toolchain from a local naiveproxy
// checkout; else falls back to a system clang-cl via PANGEA_CLANG_CL, the
// default LLVM install dir, or PATH. Returns the clang.exe path, or null when
// none is found. Native-COFF pangea_naive.lib links against any recent clang.
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

// clang.exe and clang-cl.exe are the same binary; cgo invokes it as clang.exe
// (argv[0] selects the GCC-style driver). Returns an existing sibling
// clang.exe, else copies clang-cl.exe to one (the pinned toolchain dir is
// writable; the system LLVM dir already ships clang.exe, so no copy needed).
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

// Builds (or reuses a cached) naive-cc-wrapper.exe — see
// scripts/naive-cc-wrapper/main.go for what it does and why it must be a
// real compiled executable rather than a .bat/.cmd file (Go's os/exec
// cannot invoke those directly as CC).
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

// Locates vcvarsall.bat and runs it to capture INCLUDE/LIB for the given
// architecture ("x64" | "arm64"). Does not rely on any prior interactive
// setup.
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

// Finds vcvarsall.bat via vswhere (any VS version/edition — VS 2026 installs
// under a major-version root like "...\Microsoft Visual Studio\18\Enterprise",
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
