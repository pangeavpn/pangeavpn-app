/** The KEM both ends name on the wire. A different KEM gets a different name. */
export const PQ_ALGORITHM = "ml-kem-768";

/** The daemon's half of a WireGuard pre-shared key exchange, carried to the hub. */
export interface PostQuantumOffer {
  id: string;
  algorithm: string;
  kemPublicKey: string;
}

/** The node's half, carried back through the hub to the daemon. */
export interface PostQuantumAnswer {
  algorithm: string;
  kemCiphertext: string;
}

/** What the daemon returns when it encapsulates against someone else's key. */
export interface Encapsulation {
  kemCiphertext: string;
  sharedSecret: string;
}

/** The daemon routes the app leans on for post-quantum key material. */
export interface PostQuantumProvider {
  /** Null when the daemon predates the exchange; the peer then goes unkeyed. */
  pqOffer(): Promise<PostQuantumOffer | null>;
  /** The base64 pre-shared key. Throws when the answer does not fit the offer. */
  pqFinish(id: string, answer: PostQuantumAnswer): Promise<string>;
  /** Null when the daemon predates the route. */
  pqEncapsulate(algorithm: string, kemPublicKey: string): Promise<Encapsulation | null>;
}

// Null means an old server that did not take part. A present but unusable
// answer throws: the node has keyed its side, so an unkeyed tunnel would never handshake.
export function parsePostQuantumAnswer(
  offer: PostQuantumOffer | null,
  raw: unknown
): PostQuantumAnswer | null {
  if (!offer || raw === undefined || raw === null) return null;
  if (typeof raw !== "object") {
    throw new Error("Server answered the post-quantum offer with a malformed block");
  }
  const { algorithm, kemCiphertext } = raw as Record<string, unknown>;
  if (typeof kemCiphertext !== "string" || kemCiphertext === "") {
    throw new Error("Server answered the post-quantum offer without a ciphertext");
  }
  if (algorithm !== offer.algorithm) {
    throw new Error(`Server answered the post-quantum offer with ${String(algorithm)}, not ${offer.algorithm}`);
  }
  return { algorithm: offer.algorithm, kemCiphertext };
}
