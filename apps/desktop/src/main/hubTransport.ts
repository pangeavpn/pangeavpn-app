import net from "node:net";
import tls from "node:tls";
import https from "node:https";

/** Status code from a CONNECT response head; throws if it is not HTTP. */
export function parseConnectStatus(head: string): number {
  const statusLine = head.split(/\r?\n/, 1)[0] ?? "";
  const match = /^HTTP\/1\.[01]\s+(\d{3})/.exec(statusLine);
  if (!match) {
    throw new Error(`Malformed CONNECT response: ${statusLine.slice(0, 80)}`);
  }
  return Number(match[1]);
}

const MAX_CONNECT_HEAD = 8192;
const MAX_RESPONSE_BODY = 25 * 1024 * 1024;

// host:port or bare host, no CRLF/whitespace/control characters — used for
// both the CONNECT target and the Host header, since an injected value in
// either opens a tunnel to an arbitrary destination.
const SAFE_HOST_PATTERN = /^[A-Za-z0-9.:_-]+$/;

function isSafeHost(value: string): boolean {
  return SAFE_HOST_PATTERN.test(value);
}

/** Sends CONNECT. Bytes after the head are unshifted back so the TLS handshake
 *  that follows keeps its first record. */
function performConnect(socket: net.Socket, target: string, proxyAuthHeader?: string): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    if (!isSafeHost(target)) {
      reject(new Error("Invalid CONNECT target"));
      return;
    }
    let buf = Buffer.alloc(0);

    const cleanup = (): void => {
      socket.removeListener("data", onData);
      socket.removeListener("error", onError);
      socket.removeListener("end", onEnd);
    };
    const onError = (err: Error): void => {
      cleanup();
      reject(err);
    };
    const onEnd = (): void => {
      cleanup();
      reject(new Error("Proxy closed the connection during CONNECT"));
    };
    const onData = (chunk: Buffer): void => {
      buf = Buffer.concat([buf, chunk]);
      const idx = buf.indexOf("\r\n\r\n");
      if (idx === -1) {
        if (buf.length > MAX_CONNECT_HEAD) {
          cleanup();
          reject(new Error("CONNECT response head too large"));
        }
        return;
      }

      const head = buf.subarray(0, idx).toString("latin1");
      const rest = buf.subarray(idx + 4);
      cleanup();

      let status: number;
      try {
        status = parseConnectStatus(head);
      } catch (err) {
        reject(err as Error);
        return;
      }
      if (status < 200 || status > 299) {
        reject(new Error(`Proxy refused CONNECT to ${target} (status ${status})`));
        return;
      }
      if (rest.length > 0) {
        socket.unshift(rest);
      }
      resolve();
    };

    socket.on("data", onData);
    socket.on("error", onError);
    socket.on("end", onEnd);

    const authLine = proxyAuthHeader ? `Proxy-Authorization: ${proxyAuthHeader}\r\n` : "";
    socket.write(`CONNECT ${target} HTTP/1.1\r\nHost: ${target}\r\n${authLine}\r\n`);
  });
}

function buildProxyAuthHeader(username?: string, password?: string): string | undefined {
  if (!username || !password) {
    return undefined;
  }
  return `Basic ${Buffer.from(`${username}:${password}`).toString("base64")}`;
}

export interface ConnectProxyOptions {
  method?: string;
  headers?: Record<string, string>;
  body?: string;
  timeoutMs?: number;
  /** Extra trust anchor. Tests supply their self-signed cert; production
   *  passes nothing and validates against the system store. */
  ca?: string;
  signal?: AbortSignal;
  /** Basic-auth credentials for the daemon's mixed-inbound CONNECT proxy. */
  proxyUsername?: string;
  proxyPassword?: string;
}

/** HTTPS to `target` (hostname or IP) through a local CONNECT proxy. Unlike
 *  fetchDohResolved this validates the certificate normally: that one dials an
 *  IP with an empty SNI to hide the host from DPI, whereas here the tunnel
 *  already hides it, so there is nothing to trade the check away for. */
export function fetchViaConnectProxy(
  proxyPort: number,
  ip: string,
  hostname: string,
  requestPath: string,
  options: ConnectProxyOptions = {}
): Promise<Response> {
  const deadline = options.timeoutMs ?? 15000;
  const target = `${ip}:443`;
  const proxyAuthHeader = buildProxyAuthHeader(options.proxyUsername, options.proxyPassword);

  return new Promise<Response>((resolve, reject) => {
    let settled = false;
    let socket: net.Socket | null = null;
    let tlsSocket: tls.TLSSocket | null = null;

    const fail = (err: Error): void => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      options.signal?.removeEventListener("abort", onAbort);
      tlsSocket?.destroy();
      socket?.destroy();
      reject(err);
    };

    const onAbort = (): void => fail(new Error("Request aborted"));

    if (!isSafeHost(hostname) || !isSafeHost(target)) {
      reject(new Error("Invalid host"));
      return;
    }

    if (options.signal) {
      if (options.signal.aborted) {
        reject(new Error("Request aborted"));
        return;
      }
      options.signal.addEventListener("abort", onAbort, { once: true });
    }

    const timer = setTimeout(() => fail(new Error("Request timeout")), deadline);

    socket = net.connect(proxyPort, "127.0.0.1");
    socket.once("error", fail);

    socket.once("connect", () => {
      performConnect(socket as net.Socket, target, proxyAuthHeader)
        .then(() => {
          if (settled) return;
          tlsSocket = tls.connect({
            socket: socket as net.Socket,
            servername: hostname,
            ...(options.ca ? { ca: options.ca } : {})
          });
          tlsSocket.once("error", fail);

          const req = https.request(
            {
              // No `agent` key at all: Node consults createConnection only
              // when the request has no agent. Passing agent:false makes it
              // build one, which then dials host:port itself and ignores this.
              createConnection: () => tlsSocket as tls.TLSSocket,
              // Ignored because createConnection supplies the socket, but the
              // client still builds a request authority from them.
              host: ip,
              port: 443,
              path: requestPath,
              method: options.method ?? "GET",
              headers: { ...options.headers, Host: hostname }
            },
            (res) => {
              const chunks: Buffer[] = [];
              let bodyLength = 0;
              res.on("data", (chunk: Buffer) => {
                bodyLength += chunk.length;
                if (bodyLength > MAX_RESPONSE_BODY) {
                  fail(new Error("Response body too large"));
                  res.destroy();
                  return;
                }
                chunks.push(chunk);
              });
              res.on("end", () => {
                if (settled) return;
                settled = true;
                clearTimeout(timer);
                options.signal?.removeEventListener("abort", onAbort);
                const headers = new Headers();
                for (const [key, value] of Object.entries(res.headers)) {
                  if (value) {
                    headers.set(key, Array.isArray(value) ? value.join(", ") : value);
                  }
                }
                const body = Buffer.concat(chunks).toString("utf8");
                // One-shot request: nothing reuses these, and leaving them open
                // keeps the event loop alive.
                tlsSocket?.destroy();
                socket?.destroy();
                resolve(new Response(body, { status: res.statusCode ?? 500, headers }));
              });
            }
          );

          req.on("error", fail);
          if (options.body) {
            req.write(options.body);
          }
          req.end();
        })
        .catch(fail);
    });
  });
}
