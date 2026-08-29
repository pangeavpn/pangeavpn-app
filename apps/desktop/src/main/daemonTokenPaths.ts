import path from "node:path";

const APP_FOLDER = "pangeavpn-desktop";
const MAC_SYSTEM_FOLDER = "PangeaVPN";
const TOKEN_FILE = "daemon-token.txt";

// A daemon started without PANGEA_APP_SUPPORT_DIR — a dev `go run`, or any
// manual launch — writes its token under the compiled-in default instead.
export function daemonTokenCandidatePaths(
  platform: NodeJS.Platform,
  tokenPath: string,
  userAppDataDir: string
): string[] {
  const candidates: string[] = [];
  const seen = new Set<string>();
  const add = (candidate: string) => {
    const normalized = path.normalize(candidate);
    if (seen.has(normalized)) {
      return;
    }
    seen.add(normalized);
    candidates.push(normalized);
  };

  add(tokenPath);

  if (platform === "darwin") {
    add(path.join("/Library/Application Support", MAC_SYSTEM_FOLDER, TOKEN_FILE));
    add(path.join("/Library/Application Support", APP_FOLDER, TOKEN_FILE));
    add(path.join(userAppDataDir, APP_FOLDER, TOKEN_FILE));
  }

  if (platform === "linux") {
    add(path.join("/etc/pangeavpn", TOKEN_FILE));
    add(path.join("/var/lib", APP_FOLDER, TOKEN_FILE));
    // Last, so a systemd-managed install authenticates with the root daemon's
    // token before ever trying a stale one left in the user's own directory.
    add(path.join(userAppDataDir, APP_FOLDER, TOKEN_FILE));
  }

  return candidates;
}
