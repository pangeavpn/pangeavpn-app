import tls from "node:tls";

type ReadRoots = (store: "bundled" | "system") => string[];

const pemBody = (pem: string): string => pem.replace(/-----[^-]+-----|\s/g, "");

// The roots this user's OS trusts beyond Node's bundled set, one PEM bundle. It
// includes the user's own store, which the root/SYSTEM daemon cannot read.
export function userTrustedRootsPem(read: ReadRoots = (store) => tls.getCACertificates(store)): string {
  try {
    const bundled = new Set(read("bundled").map(pemBody));
    return read("system")
      .filter((pem) => !bundled.has(pemBody(pem)))
      .join("\n");
  } catch {
    return "";
  }
}
