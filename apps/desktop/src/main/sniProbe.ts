import { createHash } from "node:crypto";
import { connect } from "node:tls";

/** The node address and the pins its self-signed certificate must hold. */
export interface SniProbeTarget {
  host: string;
  port: number;
  pins: string[];
}

export type SniProbeVerdict = "node" | "intercepted" | "failed";

export interface SniProbeOutcome {
  sni: string;
  verdict: SniProbeVerdict;
  issuer?: string;
  subject?: string;
  pin?: string;
  ms: number;
  error?: string;
}

const PROBE_TIMEOUT_MS = 4000;

const spkiPin = (pubkey?: Buffer): string =>
  pubkey && pubkey.length > 0 ? createHash("sha256").update(pubkey).digest("base64") : "";

const name = (value?: { CN?: string | string[]; O?: string | string[] }): string =>
  [value?.CN, value?.O]
    .flatMap((part) => (Array.isArray(part) ? part : [part]))
    .filter(Boolean)
    .join(" | ");

/** A pin match means the node answered and nothing decrypted the session; a
 *  mismatch means something else finished the handshake, and the issuer says what. */
export function probeSni(
  target: SniProbeTarget,
  sni: string,
  timeoutMs = PROBE_TIMEOUT_MS
): Promise<SniProbeOutcome> {
  const startedAt = Date.now();
  return new Promise((resolve) => {
    const socket = connect({
      host: target.host,
      port: target.port,
      servername: sni,
      rejectUnauthorized: false,
      timeout: timeoutMs
    });
    const finish = (verdict: SniProbeVerdict, extra: Partial<SniProbeOutcome>) => {
      socket.destroy();
      resolve({ sni, verdict, ms: Date.now() - startedAt, ...extra });
    };

    socket.once("timeout", () => finish("failed", { error: "timeout" }));
    socket.once("error", (err: Error) => finish("failed", { error: err.message }));
    socket.once("secureConnect", () => {
      const cert = socket.getPeerCertificate();
      const pin = spkiPin(cert?.pubkey);
      const issuer = name(cert?.issuer);
      const subject = name(cert?.subject);
      // With no pin to compare, a self-signed certificate can only be the node.
      const selfSigned = issuer.length > 0 && issuer === subject;
      const verdict = target.pins.includes(pin) || (!target.pins.length && selfSigned) ? "node" : "intercepted";
      finish(verdict, { issuer, subject, ...(pin ? { pin } : {}) });
    });
  });
}

/** One line per name: a blocked name reads differently from one a proxy decrypts
 *  and from one that reaches the node untouched. */
export async function collectSniProbe(
  target: SniProbeTarget,
  snis: readonly string[]
): Promise<string> {
  const outcomes = await Promise.all(snis.map((sni) => probeSni(target, sni)));
  const lines = outcomes.map((outcome) => {
    const detail: string[] = [outcome.verdict];
    if (outcome.issuer) detail.push(`issuer="${outcome.issuer}"`);
    if (outcome.subject) detail.push(`subject="${outcome.subject}"`);
    if (outcome.verdict === "intercepted" && outcome.pin) detail.push("pin-mismatch");
    if (outcome.error) detail.push(outcome.error);
    detail.push(`${outcome.ms}ms`);
    return `${outcome.sni} | ${detail.join(" | ")}`;
  });
  return [`## tls sni probe (${target.host}:${target.port})`, ...lines].join("\n");
}
