/** Hosts (and their subdomains) the app may hand to the OS browser. */
export const ALLOWED_EXTERNAL_HOSTS = ["github.com", "pangeavpn.org", "pangeavpn.it"];

/**
 * Whether a URL may be opened outside the app. Every caller is nominally
 * internal, but shell.openExternal reaches the OS, so the renderer's IPC and
 * the window-open handler are held to the same list as release links.
 */
export function isSafeExternalUrl(url: unknown): url is string {
  if (typeof url !== "string") return false;
  try {
    const parsed = new URL(url);
    if (parsed.protocol !== "https:" && parsed.protocol !== "http:") return false;
    return ALLOWED_EXTERNAL_HOSTS.some(
      (host) => parsed.hostname === host || parsed.hostname.endsWith(`.${host}`)
    );
  } catch {
    return false;
  }
}
