/** Pinned Ed25519 verify keys for the dead-drop bootstrap file, raw 32 bytes
 *  base64. The private halves are held offline and never enter this repository. */

import type { DeadDropKeys } from "../shared/deadDropBlob";

/** `reserve` is unused until `active` is compromised: re-signing with it moves
 *  every installed client without an emergency release. */
export const DEAD_DROP_KEYS: DeadDropKeys = {
  active: "d1fIudP+7WehrFOqar8LxneSKuvBSQlIHsqKgQXTJFQ=",
  reserve: "btSYGcZsOJ+G1UkSYiowPrFnbRA3yt12QwMI7XEmpS0="
};
