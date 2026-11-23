import { timingSafeEqual } from "node:crypto";
import http from "node:http";
import type { AddressInfo } from "node:net";
import { normalizeDesktopLinkCode } from "./desktopLink.ts";

// One-shot receiver for the browser sign-in redirect
// (http://127.0.0.1:<port>/callback?code=…&state=…). It accepts exactly one
// request whose state matches, then closes. Anything else is answered and
// ignored, so a stray or hostile request cannot end the wait.

export interface LoopbackPageText {
  /** Shown after a good callback. */
  done: string;
  /** Shown when the request was not a valid callback for this sign-in. */
  invalid: string;
}

export const DEFAULT_LOOPBACK_TEXT: LoopbackPageText = {
  done: "Approved. Return to PangeaVPN to finish signing in. You can close this tab.",
  invalid: "This sign-in link doesn't match the PangeaVPN app. Start again from the app."
};

export interface LoopbackCallbackOptions {
  state: string;
  timeoutMs: number;
  text?: LoopbackPageText;
}

export class LoopbackClosedError extends Error {
  readonly reason: "timeout" | "closed";
  constructor(reason: "timeout" | "closed") {
    super(reason === "timeout" ? "browser sign-in timed out" : "browser sign-in was cancelled");
    this.name = "LoopbackClosedError";
    this.reason = reason;
  }
}

export interface LoopbackCallback {
  port: number;
  /** The normalised code from the one accepted callback. */
  code: Promise<string>;
  /** Stops listening; a pending `code` rejects with reason "closed". */
  close(): void;
}

const SECURITY_HEADERS: Record<string, string> = {
  "Content-Type": "text/html; charset=utf-8",
  "Cache-Control": "no-store",
  "Referrer-Policy": "no-referrer",
  "Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'",
  "X-Content-Type-Options": "nosniff",
  "X-Frame-Options": "DENY"
};

function escapeHtml(text: string): string {
  return text.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
}

function page(message: string): string {
  return (
    '<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">' +
    "<title>PangeaVPN</title><style>body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;" +
    "font:16px/1.5 system-ui,sans-serif;background:#0f1115;color:#e8eaed;padding:16px;box-sizing:border-box}" +
    "p{max-width:28rem;text-align:center}@media (prefers-color-scheme:light){body{background:#fff;color:#1a1c20}}</style>" +
    `</head><body><p>${escapeHtml(message)}</p></body></html>`
  );
}

function sameSecret(a: string, b: string): boolean {
  const left = Buffer.from(a, "utf8");
  const right = Buffer.from(b, "utf8");
  return left.length === right.length && timingSafeEqual(left, right);
}

export function startLoopbackCallback(options: LoopbackCallbackOptions): Promise<LoopbackCallback> {
  const text = options.text ?? DEFAULT_LOOPBACK_TEXT;
  let settle: { resolve: (code: string) => void; reject: (err: Error) => void } | null = null;
  const code = new Promise<string>((resolve, reject) => {
    settle = { resolve, reject };
  });
  // The caller may only ever look at the success path; never leak an unhandled rejection.
  code.catch(() => {});

  let port = 0;
  let finished = false;
  const server = http.createServer((req, res) => {
    const reply = (status: number, message: string) => {
      res.writeHead(status, SECURITY_HEADERS);
      res.end(page(message));
    };

    // Only the exact origin the website redirects to; a DNS-rebound name
    // pointing at 127.0.0.1 carries a different Host.
    if (finished || req.headers.host !== `127.0.0.1:${port}`) {
      reply(404, text.invalid);
      return;
    }
    let url: URL;
    try {
      url = new URL(req.url ?? "/", `http://127.0.0.1:${port}`);
    } catch {
      reply(400, text.invalid);
      return;
    }
    if (req.method !== "GET" || url.pathname !== "/callback") {
      reply(404, text.invalid);
      return;
    }
    const state = url.searchParams.get("state");
    const normalized = normalizeDesktopLinkCode(url.searchParams.get("code"));
    if (!state || !sameSecret(state, options.state) || !normalized) {
      reply(400, text.invalid);
      return;
    }

    finished = true;
    res.writeHead(200, { ...SECURITY_HEADERS, Connection: "close" });
    res.end(page(text.done), () => shutdown());
    settle?.resolve(normalized);
  });

  let timer: NodeJS.Timeout | null = null;
  const shutdown = () => {
    if (timer) clearTimeout(timer);
    timer = null;
    server.close();
    server.closeAllConnections();
  };
  const fail = (reason: "timeout" | "closed") => {
    if (finished) return;
    finished = true;
    settle?.reject(new LoopbackClosedError(reason));
    shutdown();
  };

  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      server.off("error", reject);
      port = (server.address() as AddressInfo).port;
      timer = setTimeout(() => fail("timeout"), options.timeoutMs);
      timer.unref?.();
      resolve({ port, code, close: () => fail("closed") });
    });
  });
}
