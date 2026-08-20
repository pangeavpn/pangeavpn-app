import assert from "node:assert/strict";
import test from "node:test";
import net from "node:net";
import https from "node:https";
import type { IncomingMessage, ServerResponse } from "node:http";
import { DOH_TLS_OPTIONS, fetchViaConnectProxy, parseConnectStatus } from "./hubTransport.ts";

test("parseConnectStatus reads the status code", () => {
  assert.equal(parseConnectStatus("HTTP/1.1 200 Connection established"), 200);
  assert.equal(parseConnectStatus("HTTP/1.0 200 OK"), 200);
  assert.equal(parseConnectStatus("HTTP/1.1 403 Forbidden\r\nX: y"), 403);
  assert.equal(parseConnectStatus("HTTP/1.1 502 Bad Gateway"), 502);
});

test("parseConnectStatus rejects anything that is not a status line", () => {
  assert.throws(() => parseConnectStatus("<html>captive portal</html>"), /Malformed/);
  assert.throws(() => parseConnectStatus(""), /Malformed/);
  assert.throws(() => parseConnectStatus("220 smtp.example.com ESMTP"), /Malformed/);
});

// Self-signed localhost cert (CN=localhost, SAN 127.0.0.1), valid to 2036.
// Passed as `ca` so the tests validate against it, exactly as production
// validates the hub against the system store.
const TEST_CERT = `-----BEGIN CERTIFICATE-----
MIIDJTCCAg2gAwIBAgIUAQ53rDPhTKZ1zz28ttwM2ShAHWgwDQYJKoZIhvcNAQEL
BQAwFDESMBAGA1UEAwwJbG9jYWxob3N0MB4XDTI2MDgwNzIyNDMyNFoXDTM2MDgw
NDIyNDMyNFowFDESMBAGA1UEAwwJbG9jYWxob3N0MIIBIjANBgkqhkiG9w0BAQEF
AAOCAQ8AMIIBCgKCAQEAxvAF2IzBsvBgPYNXpreO5c6fXBi75iDOfSZfU0dah/T9
xstmRYGLyRrrPqomoGrFlspK07dC1scxWivBAI/ISWwQ4l0/OIHF/aZyD+ptdhNA
KYQ/ucopQCp4chILQOP1fDdCHwnEPwMN/BA+45BO3kQVeJrvxkVy+5UC7PJAcaj6
A9siovdbXZsVYFlSq2C2tM1xo5rfnOUQi06sz1CO1tiMLNtp/4DwdKWz6d6deaPg
2N1VTIf0EGkgdsDaarCEPPNB9RkhaSDgjcbfjkPIIP6EKpjb7fg484zdusDvUGGl
RltbSIT/7Cey/zmQiggvOP9UL5AFr18sTUrSYdfoJwIDAQABo28wbTAdBgNVHQ4E
FgQUocNV+84VXN7+fZ2rCRHYr92YbAAwHwYDVR0jBBgwFoAUocNV+84VXN7+fZ2r
CRHYr92YbAAwDwYDVR0TAQH/BAUwAwEB/zAaBgNVHREEEzARgglsb2NhbGhvc3SH
BH8AAAEwDQYJKoZIhvcNAQELBQADggEBAJvS9JINlKWeKiNuPoFEMLUCmS6473Ih
EuWr4aqXaGVozjU2CDad1rflQeHk7fl1/NRY2HUoMVECggWXCydwedDSO7udAgP8
Lj/zn3ZznLCYIJFkztEBWIL6L61TFpYQOkWNkuiUwAK5wvsY/RKvEM7cNDhB/gyc
Q17Nq4mWgmdbwwSEM9WY3m07XudVYYAIGPRo30i40vwO0dU4IWTDadIABLh3MGNS
nq1MtOyZNq1fMxlwRQPskSqfjYfRPZHEKlmKjQvh57hlr+wkoVs9l1Q4hSJ6BCXp
jbbgQJa6+yDGJ92Np9kIIK0p6MdG0xtcLlmqmnFvyVxLVvRyhMAK24k=
-----END CERTIFICATE-----`;

const TEST_KEY = `-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDG8AXYjMGy8GA9
g1emt47lzp9cGLvmIM59Jl9TR1qH9P3Gy2ZFgYvJGus+qiagasWWykrTt0LWxzFa
K8EAj8hJbBDiXT84gcX9pnIP6m12E0AphD+5yilAKnhyEgtA4/V8N0IfCcQ/Aw38
ED7jkE7eRBV4mu/GRXL7lQLs8kBxqPoD2yKi91tdmxVgWVKrYLa0zXGjmt+c5RCL
TqzPUI7W2Iws22n/gPB0pbPp3p15o+DY3VVMh/QQaSB2wNpqsIQ880H1GSFpIOCN
xt+OQ8gg/oQqmNvt+DjzjN26wO9QYaVGW1tIhP/sJ7L/OZCKCC84/1QvkAWvXyxN
StJh1+gnAgMBAAECggEADBAl6pmubTFSRKigOgXLbnf3BdiiHDRFESWwhhY/kRr0
AIf47aILXeh591TN/tA6pwghPXFRZkCx52vbyjLtzDX3WCKbYMvNu7HKHNj0RkKo
k1vnmVZ+5dstbo1VjVvFWQDoy4UGF2QSBwTdK2NmxOeP/b43Z+hyLns8sC2IZtvi
rB2snVO5o3FZ1fhoF6RZfOOLHXvWGx3kb7yPAAbvj1f199oF7Qcc2hOEgbLsQwsc
oZ4QU/LotdBromh6ubII0TIxCu9D3GFMItu6kXWvcPreytB2IYtu/NLcxfk92axC
m6bu3rNOzP4KGV2Ncpz+XLCsme9Vayg1SaO6gl8g0QKBgQD+fnlI7DPR/u92Pk9E
gs+FSrcWwVdYoDJhIwbbCcFbCskip6VD8WmylE3W1LIXsGPwODWGUNUvYk4NDtwq
8ZYWPLAUBorsCvn1efpGghWCIZeuRZDK/J8V2wED6IeMsNaQLl/f5PmWOtVX1I3m
frVTCe/U2ECddYVwONz1A34tUwKBgQDIHWNZdMbz+NM4n0DvnvC3tkrroYatk7IR
3PFzbMtlYavdfHN9VtEpcYTrTiglCAiwjGIcaedKn9xeFANGlo7dn7K8vXLX+8ZB
xmBpTRk3AnlGIeQPRHn006C/nQNp6HVKcCMAX2kLnzr3WyLJV08wl70WTJ7xBQ0c
t7rFZfmrXQKBgQC8RA+xNJt5RCEd1iaJxkOCla0wNkNJmujqFyFhNKxHj4kQC/kk
dBj/NNsIjDxbbe/gq5RdErtC3HRlEJMraaDgPnD7v4NR7yTOxjexpVYH+JXfJDNj
FtMRNfxgScrM950i+EuQtDE3Q7rDyMhYtW+qSHWVfYz/bwsR498Bml3jZQKBgFsW
fW1vqUvOHB7u5njr6PhGgs3EpXAHBYv5/PGkOOT502gqyMrppKVvpagR2FYa1RG/
pLz4O66NG5q7E06jI36fvZUJyuejE/hGmwXzcSHH/3m73XpRmg2l8sqlZrNje1gZ
uOTniQIgRY/oLOpm0oX02731vHdK7FABFYPayg2FAoGAUeiqz9HhiN8VN4wECby9
2U6m9iNfV7xCKZ/+4kvR4iRJ3Jzi5LnYT4AQ6iWcIXXHmJtGBS/FJQ7BE1wRYURc
VotJKKB4wRcTvXNS85n50/F77Y11VLxMgvM30wDICByaYXZ7lPZoa2NAMU5dQgOz
1s3vso2O8p21HDibSEv5GrA=
-----END PRIVATE KEY-----`;

/**
 * server.close() only calls back once every connection has gone, and a spliced
 * proxy keeps sockets alive past the request — so live sockets are tracked and
 * destroyed explicitly. server.closeAllConnections() is not present on every
 * Node this repo runs on, and optional-calling it hides the hang rather than
 * fixing it.
 */
function tracker(server: net.Server | https.Server): {
  add: (socket: net.Socket) => void;
  close: () => Promise<void>;
} {
  const live = new Set<net.Socket>();
  const add = (socket: net.Socket): void => {
    live.add(socket);
    socket.on("close", () => live.delete(socket));
  };
  server.on("connection", add);
  return {
    add,
    close: () =>
      new Promise<void>((resolve) => {
        for (const socket of live) socket.destroy();
        live.clear();
        server.close(() => resolve());
      })
  };
}

interface Started {
  port: number;
  close: () => Promise<void>;
}

/**
 * CONNECT proxy standing in for the daemon's mixed inbound. `dialPort`
 * overrides where it actually connects, since fetchViaConnectProxy always
 * names :443 for the real hub.
 */
async function startProxy(
  opts: { status?: number; dialPort?: number } = {}
): Promise<Started & { seen: string[]; heads: string[] }> {
  const status = opts.status ?? 200;
  const seen: string[] = [];
  const heads: string[] = [];
  let track: ReturnType<typeof tracker>;

  const server = net.createServer((client) => {
    client.once("data", (chunk) => {
      const head = chunk.toString("latin1");
      seen.push(head.split("\r\n")[0]);
      heads.push(head);
      if (status !== 200) {
        client.end(`HTTP/1.1 ${status} Denied\r\n\r\n`);
        return;
      }
      const target = /^CONNECT +(\S+)/.exec(head)?.[1] ?? "";
      const sep = target.lastIndexOf(":");
      const port = opts.dialPort ?? Number(target.slice(sep + 1));
      const upstream = net.connect(port, "127.0.0.1", () => {
        client.write("HTTP/1.1 200 Connection established\r\n\r\n");
        client.pipe(upstream);
        upstream.pipe(client);
      });
      track.add(upstream);
      upstream.on("error", () => client.destroy());
    });
    client.on("error", () => {});
  });

  track = tracker(server);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  return { port: (server.address() as net.AddressInfo).port, close: () => track.close(), seen, heads };
}

async function startTlsOrigin(
  handler: (req: IncomingMessage, res: ServerResponse) => void
): Promise<Started & { hosts: string[] }> {
  const hosts: string[] = [];
  const server = https.createServer({ cert: TEST_CERT, key: TEST_KEY }, (req, res) => {
    hosts.push(req.headers.host ?? "");
    handler(req, res);
  });
  const track = tracker(server);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  return { port: (server.address() as net.AddressInfo).port, close: () => track.close(), hosts };
}

test("carries a POST through the proxy and returns the origin's response", async () => {
  const origin = await startTlsOrigin((req, res) => {
    const chunks: Buffer[] = [];
    req.on("data", (c: Buffer) => chunks.push(c));
    req.on("end", () => {
      res.writeHead(200, { "content-type": "application/json" });
      res.end(JSON.stringify({ echoed: Buffer.concat(chunks).toString("utf8"), path: req.url }));
    });
  });
  const proxy = await startProxy({ dialPort: origin.port });
  try {
    const res = await fetchViaConnectProxy(proxy.port, "203.0.113.9", "localhost", "/v1/secure", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: '{"eph":"x"}',
      timeoutMs: 8000, ca: TEST_CERT
    });
    assert.equal(res.status, 200);
    const body = (await res.json()) as { echoed: string; path: string };
    assert.equal(body.echoed, '{"eph":"x"}');
    assert.equal(body.path, "/v1/secure");
    assert.equal(origin.hosts[0], "localhost", "Host header must name the hub, not the address");
    assert.equal(proxy.seen[0], "CONNECT 203.0.113.9:443 HTTP/1.1");
  } finally {
    await proxy.close();
    await origin.close();
  }
});

test("surfaces a non-2xx status from the origin rather than throwing", async () => {
  const origin = await startTlsOrigin((_req, res) => {
    res.writeHead(401);
    res.end("nope");
  });
  const proxy = await startProxy({ dialPort: origin.port });
  try {
    const res = await fetchViaConnectProxy(proxy.port, "203.0.113.9", "localhost", "/v1/secure", {
      timeoutMs: 8000, ca: TEST_CERT
    });
    assert.equal(res.status, 401);
    assert.equal(await res.text(), "nope");
  } finally {
    await proxy.close();
    await origin.close();
  }
});

test("rejects when the proxy refuses CONNECT", async () => {
  const proxy = await startProxy({ status: 403 });
  try {
    await assert.rejects(
      fetchViaConnectProxy(proxy.port, "203.0.113.9", "localhost", "/v1/secure", { timeoutMs: 3000, ca: TEST_CERT }),
      /status 403/
    );
    assert.equal(proxy.seen[0], "CONNECT 203.0.113.9:443 HTTP/1.1");
  } finally {
    await proxy.close();
  }
});

test("honours its timeout when the proxy accepts but never answers", async () => {
  const server = net.createServer(() => {
    // accept, say nothing
  });
  const track = tracker(server);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const port = (server.address() as net.AddressInfo).port;
  try {
    const started = Date.now();
    await assert.rejects(
      fetchViaConnectProxy(port, "203.0.113.9", "localhost", "/v1/secure", { timeoutMs: 300, ca: TEST_CERT }),
      /timeout/i
    );
    assert.ok(Date.now() - started < 3000, "must reject on its own timer rather than hang");
  } finally {
    await track.close();
  }
});

test("rejects a proxy that answers with something other than HTTP", async () => {
  const server = net.createServer((client) => {
    client.write("<html>captive portal</html>\r\n\r\n");
  });
  const track = tracker(server);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const port = (server.address() as net.AddressInfo).port;
  try {
    await assert.rejects(
      fetchViaConnectProxy(port, "203.0.113.9", "localhost", "/v1/secure", { timeoutMs: 3000, ca: TEST_CERT }),
      /Malformed/
    );
  } finally {
    await track.close();
  }
});

test("fails when nothing is listening on the proxy port", async () => {
  const probe = net.createServer();
  await new Promise<void>((resolve) => probe.listen(0, "127.0.0.1", resolve));
  const deadPort = (probe.address() as net.AddressInfo).port;
  await new Promise<void>((resolve) => probe.close(() => resolve()));

  await assert.rejects(
    fetchViaConnectProxy(deadPort, "203.0.113.9", "localhost", "/v1/secure", { timeoutMs: 3000, ca: TEST_CERT })
  );
});

test("rejects a certificate that does not chain to the supplied trust anchor", async () => {
  const origin = await startTlsOrigin((_req, res) => res.end("secret"));
  const proxy = await startProxy({ dialPort: origin.port });
  try {
    // No `ca`, so the self-signed fixture is untrusted, as an attacker's cert
    // would be. Before validation was enabled this resolved happily.
    await assert.rejects(
      fetchViaConnectProxy(proxy.port, "203.0.113.9", "localhost", "/v1/secure", { timeoutMs: 5000 }),
      /self.signed|unable to verify|certificate/i
    );
  } finally {
    await proxy.close();
    await origin.close();
  }
});

test("sends Proxy-Authorization when credentials are supplied", async () => {
  const origin = await startTlsOrigin((_req, res) => res.end("ok"));
  const proxy = await startProxy({ dialPort: origin.port });
  try {
    await fetchViaConnectProxy(proxy.port, "203.0.113.9", "localhost", "/v1/secure", {
      timeoutMs: 8000,
      ca: TEST_CERT,
      proxyUsername: "user",
      proxyPassword: "pass"
    });
    const expected = `Basic ${Buffer.from("user:pass").toString("base64")}`;
    assert.match(proxy.heads[0], new RegExp(`Proxy-Authorization: ${expected}`));
  } finally {
    await proxy.close();
    await origin.close();
  }
});

test("aborts the in-flight request via signal", async () => {
  const server = net.createServer(() => {
    // accept, say nothing, so the request stays pending until aborted
  });
  const track = tracker(server);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const port = (server.address() as net.AddressInfo).port;
  const controller = new AbortController();
  try {
    const pending = assert.rejects(
      fetchViaConnectProxy(port, "203.0.113.9", "localhost", "/v1/secure", {
        timeoutMs: 8000,
        ca: TEST_CERT,
        signal: controller.signal
      }),
      /aborted/i
    );
    controller.abort();
    await pending;
  } finally {
    await track.close();
  }
});

test("rejects a certificate valid for a different host", async () => {
  const origin = await startTlsOrigin((_req, res) => res.end("secret"));
  const proxy = await startProxy({ dialPort: origin.port });
  try {
    // Trusted anchor, wrong name: the fixture cert is CN=localhost.
    await assert.rejects(
      fetchViaConnectProxy(proxy.port, "203.0.113.9", "api.pangeavpn.org", "/v1/secure", {
        timeoutMs: 5000,
        ca: TEST_CERT
      }),
      /altnames|does not match|Hostname/i
    );
  } finally {
    await proxy.close();
    await origin.close();
  }
});

test("the DoH path keeps its empty SNI and skips certificate validation", () => {
  // Regression: 6299892 set real SNI and restored validation, which put the
  // hub name on the wire and let any TLS middlebox veto the path.
  assert.equal(DOH_TLS_OPTIONS.servername, "");
  assert.equal(DOH_TLS_OPTIONS.rejectUnauthorized, false);
});
