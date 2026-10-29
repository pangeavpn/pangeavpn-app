// Where a release's macOS installer lives and how to check a download against
// the release's SHA256SUMS.txt. Kept free of Electron so the rules are testable.

const RELEASE_DOWNLOAD_BASE = "https://github.com/pangeavpn/pangeavpn-app/releases/download";
// The version comes from the hub reply and ends up in a URL, so only plain
// semver gets through.
const SAFE_VERSION = /^\d+\.\d+\.\d+(?:-[0-9A-Za-z]+(?:\.[0-9A-Za-z]+)*)?$/;

export interface MacPackageAsset {
  name: string;
  url: string;
  checksumsUrl: string;
}

/** The signed pkg for this Mac's architecture, as CI names and publishes it. */
export function macPackageAsset(version: string, arch: string): MacPackageAsset | null {
  const plain = version.trim().replace(/^v/, "");
  if (!SAFE_VERSION.test(plain)) return null;
  if (arch !== "arm64" && arch !== "x64") return null;
  const name = `PangeaVPN-${plain}-${arch}.pkg`;
  const base = `${RELEASE_DOWNLOAD_BASE}/v${plain}`;
  return { name, url: `${base}/${name}`, checksumsUrl: `${base}/SHA256SUMS.txt` };
}

/** The digest `sha256sum` recorded for `name`, lowercased, or null. */
export function expectedDigest(checksums: string, name: string): string | null {
  for (const line of checksums.split(/\r?\n/)) {
    const match = /^([0-9a-fA-F]{64}) [ *](.+)$/.exec(line.trim());
    if (match && match[2] === name) return match[1].toLowerCase();
  }
  return null;
}
