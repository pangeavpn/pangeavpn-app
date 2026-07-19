import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";

// The Windows default_libs list Chromium executables get automatically
// (build/config/BUILD.gn:config("default_libs")) plus the pangea_naive
// target's own transitive libs, read off a real GN executable link line.
// See docs/superpowers/specs/2026-07-18-naiveproxy-transport-design.md's
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
// "arm64"). Returns null (after logging why) if the naiveproxy fork's
// build artifacts or the matching pinned toolchain aren't available on
// this machine — callers should fall back to building without the
// naive_cgo tag (the stub transport) rather than failing the build, since
// most dev machines and CI runners won't have this multi-GB toolchain set
// up. See docs/superpowers/specs/2026-07-18-naiveproxy-transport-design.md
// for the one-time machine setup this depends on.
export function resolveNaiveCgoConfig(goArch, rootDir) {
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

  const libPath = path.join(naiveproxySrc, "out", gnArchDir, "obj", "net", "pangea_naive.lib");
  const headerDir = path.join(naiveproxySrc, "pangea", "capi");
  const headerPath = path.join(headerDir, "pangea_naive_capi.h");
  const llvmBinDir = path.join(naiveproxySrc, "third_party", "llvm-build", "Release+Asserts", "bin");
  const clangClPath = path.join(llvmBinDir, "clang-cl.exe");
  const clangPath = path.join(llvmBinDir, "clang.exe");

  if (!fs.existsSync(libPath)) {
    console.warn(
      `naive_cgo: ${libPath} not found; naive transport falls back to the stub for ${goArch}. ` +
        `Build it per docs/superpowers/specs/2026-07-18-naiveproxy-transport-design.md.`
    );
    return null;
  }
  if (!fs.existsSync(headerPath)) {
    console.warn(`naive_cgo: ${headerPath} not found; naive transport falls back to the stub for ${goArch}.`);
    return null;
  }
  if (!fs.existsSync(clangClPath)) {
    console.warn(
      `naive_cgo: pinned clang-cl.exe not found at ${clangClPath}; naive transport falls back to the stub for ${goArch}.`
    );
    return null;
  }
  if (!fs.existsSync(clangPath)) {
    // Copying clang-cl.exe to clang.exe exploits clang's argv[0]-based
    // driver-mode detection to get GCC-style flags from the exact pinned
    // toolchain revision (needed for LTO bitcode version matching against
    // pangea_naive.lib) — see the design spec for why.
    fs.copyFileSync(clangClPath, clangPath);
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

// Locates vcvarsall.bat across common Visual Studio 2022 editions/install
// roots and runs it to capture INCLUDE/LIB for the given architecture
// ("x64" | "arm64"). Does not rely on any prior interactive setup.
function findVcvarsEnv(arch) {
  const roots = [
    process.env["ProgramFiles(x86)"] ?? "C:\\Program Files (x86)",
    process.env["ProgramFiles"] ?? "C:\\Program Files"
  ];
  const editions = ["BuildTools", "Community", "Professional", "Enterprise"];

  let vcvarsallPath = null;
  outer: for (const root of roots) {
    for (const edition of editions) {
      const candidate = path.join(
        root,
        "Microsoft Visual Studio",
        "2022",
        edition,
        "VC",
        "Auxiliary",
        "Build",
        "vcvarsall.bat"
      );
      if (fs.existsSync(candidate)) {
        vcvarsallPath = candidate;
        break outer;
      }
    }
  }
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
