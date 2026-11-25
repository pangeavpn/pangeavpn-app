/** In the ladder's order: OS updates and pinned Apple services must survive
 *  inspection or break, so they are the likeliest to pass untouched. */
export const SNI_PROBE_CANDIDATES = [
  "swdist.apple.com",
  "swscan.apple.com",
  "gdmf.apple.com",
  "oscdn.apple.com",
  "updates.cdn-apple.com",
  "courier.push.apple.com",
  "objects.githubusercontent.com",
  "www.microsoft.com"
];
