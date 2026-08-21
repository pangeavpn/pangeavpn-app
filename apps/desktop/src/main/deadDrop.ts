/** Fetches the dead-drop bootstrap file. Unlike the direct-IP hub path this is
 *  ordinary HTTPS with certificate validation on: these are real hosts with
 *  real certificates, and there is no hub name to keep off the wire. */

import https from "node:https";
import { verifyDeadDropBlob, type DeadDropKeys, type DeadDropPayload } from "../shared/deadDropBlob.ts";

// Must match the org that actually publishes PangeaConfig.
const DEAD_DROP_ORG = "pangeavpn";
const DEAD_DROP_REPO = "PangeaConfig";
const DEAD_DROP_FILE = "bootstrap-v1.json";

/** Two addresses, identical content. jsDelivr is a second way to the same file
 *  rather than a second source of truth. */
export const DEAD_DROP_URLS: readonly string[] = [
  `https://raw.githubusercontent.com/${DEAD_DROP_ORG}/${DEAD_DROP_REPO}/main/${DEAD_DROP_FILE}`,
  `https://cdn.jsdelivr.net/gh/${DEAD_DROP_ORG}/${DEAD_DROP_REPO}@main/${DEAD_DROP_FILE}`
];

export const DEAD_DROP_MIN_INTERVAL_MS = 15 * 60 * 1000;
const DEFAULT_TIMEOUT_MS = 6000;
const MAX_BODY_BYTES = 64 * 1024;

/** Rate limit for the fetch. A restart loop must not turn into a hammering of
 *  the publishing hosts, and a backwards clock must not lock fetching out. */
export function deadDropDue(lastAttemptMs: number, nowMs: number): boolean {
  if (!Number.isFinite(lastAttemptMs) || lastAttemptMs <= 0) return true;
  if (lastAttemptMs > nowMs) return true;
  return nowMs - lastAttemptMs >= DEAD_DROP_MIN_INTERVAL_MS;
}

/** GETs a small text body, or null for any non-200, oversized, or failed
 *  response. Never throws for an ordinary network failure. */
export function httpsGetText(url: string, timeoutMs = DEFAULT_TIMEOUT_MS): Promise<string | null> {
  return new Promise((resolve) => {
    let settled = false;
    const finish = (value: string | null): void => {
      if (settled) return;
      settled = true;
      resolve(value);
    };

    const request = https.get(url, { timeout: timeoutMs }, (response) => {
      if (response.statusCode !== 200) {
        response.destroy();
        finish(null);
        return;
      }
      const chunks: Buffer[] = [];
      let size = 0;
      response.on("data", (chunk: Buffer) => {
        size += chunk.length;
        if (size > MAX_BODY_BYTES) {
          response.destroy();
          finish(null);
          return;
        }
        chunks.push(chunk);
      });
      response.on("end", () => finish(Buffer.concat(chunks).toString("utf8")));
      response.on("error", () => finish(null));
    });

    request.on("timeout", () => {
      request.destroy();
      finish(null);
    });
    request.on("error", () => finish(null));
  });
}

export interface DeadDropFetchOptions {
  keys: DeadDropKeys;
  minSeq: number;
  nowMs: number;
  urls?: readonly string[];
  fetchText?: (url: string, timeoutMs: number) => Promise<string | null>;
  timeoutMs?: number;
}

export interface DeadDropResult {
  payload: DeadDropPayload;
  url: string;
}

/**
 * The first published file that passes every check, with the URL it came from
 * so the caller can log which host is still working.
 *
 * A host that fails, throws, or serves something unacceptable never ends the
 * search: one blocked or poisoned mirror must not cost us the other.
 */
export async function fetchDeadDropPayload(
  options: DeadDropFetchOptions
): Promise<DeadDropResult | null> {
  const urls = options.urls ?? DEAD_DROP_URLS;
  const fetchText = options.fetchText ?? httpsGetText;
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;

  for (const url of urls) {
    if (!url.startsWith("https://")) continue;
    let text: string | null;
    try {
      text = await fetchText(url, timeoutMs);
    } catch {
      continue;
    }
    if (!text) continue;

    const payload = verifyDeadDropBlob(text, {
      keys: options.keys,
      minSeq: options.minSeq,
      nowMs: options.nowMs
    });
    if (payload) return { payload, url };
  }
  return null;
}
