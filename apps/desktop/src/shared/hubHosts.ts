/** Hostnames for the hub's own domain, and which method may use which.
 *
 *  The mirror sits behind a CDN, so only the normal method may reach it: the
 *  direct-IP and DoH paths send an empty SNI, which a shared-IP edge cannot
 *  route to a certificate. HUB_HOSTNAME is the only name those paths may use.
 */

export const HUB_HOSTNAME = "api.pangeavpn.org";

export const HUB_MIRROR_HOSTNAME = "api.pangeavpn.it";

/** Tried in order by the normal method. Primary first: the mirror costs a
 *  round trip and puts a second name on the wire. */
export function normalHubHosts(): string[] {
  const hosts = [HUB_HOSTNAME, HUB_MIRROR_HOSTNAME];
  return hosts.filter((host, i) => hosts.indexOf(host) === i);
}
