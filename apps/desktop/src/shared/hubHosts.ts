/** Hostnames for the hub's own domain. The mirror sits behind a CDN with no SNI
 *  routing, so only the normal method may use it; direct-IP/DoH use HUB_HOSTNAME only. */

export const HUB_HOSTNAME = "api.pangeavpn.org";

export const HUB_MIRROR_HOSTNAME = "api.pangeavpn.it";

/** Tried in order by the normal method. Primary first: the mirror costs a
 *  round trip and puts a second name on the wire. */
export function normalHubHosts(): string[] {
  const hosts = [HUB_HOSTNAME, HUB_MIRROR_HOSTNAME];
  return hosts.filter((host, i) => hosts.indexOf(host) === i);
}
