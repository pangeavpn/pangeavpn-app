interface ParsedVersion {
  core: number[];
  prerelease: string[];
}

function parseVersion(v: string): ParsedVersion {
  const [withoutBuild] = v.trim().replace(/^v/, "").split("+");
  const dash = withoutBuild.indexOf("-");
  const core = dash === -1 ? withoutBuild : withoutBuild.slice(0, dash);
  const pre = dash === -1 ? "" : withoutBuild.slice(dash + 1);
  return {
    core: core.split(".").map((n) => parseInt(n, 10) || 0),
    prerelease: pre === "" ? [] : pre.split("."),
  };
}

function compareIdentifiers(a: string, b: string): number {
  const an = /^\d+$/.test(a);
  const bn = /^\d+$/.test(b);
  if (an && bn) return Number(a) - Number(b);
  if (an !== bn) return an ? -1 : 1;
  return a < b ? -1 : a > b ? 1 : 0;
}

// Semver precedence: a prerelease ("0.8.0-rc.1") sorts below its final release, and
// numeric prerelease parts compare as numbers (rc.10 > rc.9).
export function compareVersions(a: string, b: string): number {
  const av = parseVersion(a);
  const bv = parseVersion(b);
  const len = Math.max(av.core.length, bv.core.length);
  for (let i = 0; i < len; i++) {
    const x = av.core[i] ?? 0;
    const y = bv.core[i] ?? 0;
    if (x !== y) return x - y;
  }
  if (av.prerelease.length === 0 || bv.prerelease.length === 0) {
    return bv.prerelease.length - av.prerelease.length;
  }
  for (let i = 0; i < Math.min(av.prerelease.length, bv.prerelease.length); i++) {
    const c = compareIdentifiers(av.prerelease[i], bv.prerelease[i]);
    if (c !== 0) return c;
  }
  return av.prerelease.length - bv.prerelease.length;
}

export function isPrerelease(version: string): boolean {
  return parseVersion(version).prerelease.length > 0;
}

// Release candidates are for beta testers, who install them by hand: a stable install is
// never offered one. A candidate install is offered anything newer, including its own final.
export function shouldOfferUpdate(latest: { version: string; prerelease?: boolean }, current: string): boolean {
  if ((latest.prerelease === true || isPrerelease(latest.version)) && !isPrerelease(current)) return false;
  return compareVersions(latest.version, current) > 0;
}
