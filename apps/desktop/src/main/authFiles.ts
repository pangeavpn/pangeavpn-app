// Every credential-bearing file auth.ts keeps in the user state directory.
// Sign-out deletes them one by one; the panic wipe removes the whole
// directory, and its tests pin that each of these sits inside it.
export const AUTH_FILE_NAMES = {
  session: "auth-session.dat",
  licenseKey: "license-key.dat",
  identityKey: "identity-key.dat",
  legacyWgKey: "wg-device-key.dat",
  /** The device-names wrap key a browser sign-in was handed (app tokens only). */
  namesWrap: "names-wrap.dat"
} as const;
