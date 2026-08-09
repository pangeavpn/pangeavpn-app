/* Flag marks drawn inline — the CSP blocks remote images, and Windows has no
   colour emoji flags, so the regional-indicator characters render as letters. */

const FLAGS: Record<string, string> = {
  NL: '<rect width="30" height="21" fill="#21468B"/><rect width="30" height="14" fill="#fff"/><rect width="30" height="7" fill="#AE1C28"/>',
  DE: '<rect width="30" height="7" fill="#000"/><rect y="7" width="30" height="7" fill="#DD0000"/><rect y="14" width="30" height="7" fill="#FFCE00"/>',
  FR: '<rect width="30" height="21" fill="#ED2939"/><rect width="20" height="21" fill="#fff"/><rect width="10" height="21" fill="#002395"/>',
  SE: '<rect width="30" height="21" fill="#006AA7"/><rect x="8" width="4.5" height="21" fill="#FECC00"/><rect y="8.5" width="30" height="4.5" fill="#FECC00"/>',
  JP: '<rect width="30" height="21" fill="#fff"/><circle cx="15" cy="10.5" r="6" fill="#BC002D"/>',
  SG: '<rect width="30" height="21" fill="#fff"/><rect width="30" height="10.5" fill="#ED2939"/>',
  CA: '<rect width="30" height="21" fill="#fff"/><rect width="7.5" height="21" fill="#FF0000"/><rect x="22.5" width="7.5" height="21" fill="#FF0000"/><path d="M15 5.5l1.6 3.4 3.4-1-1.6 3.6 3.1 1.4-3.4.6.5 3.4-3.6-2.4-3.6 2.4.5-3.4-3.4-.6 3.1-1.4L9.9 8l3.5 1z" fill="#FF0000"/>',
  AU: '<rect width="30" height="21" fill="#00008B"/><rect width="15" height="10.5" fill="#00008B"/><path d="M0 0l15 10.5M15 0L0 10.5" stroke="#fff" stroke-width="2"/><path d="M7.5 0v10.5M0 5.25h15" stroke="#fff" stroke-width="3.5"/><path d="M7.5 0v10.5M0 5.25h15" stroke="#C8102E" stroke-width="2"/>',
  BR: '<rect width="30" height="21" fill="#009C3B"/><path d="M15 3.5 27 10.5 15 17.5 3 10.5z" fill="#FFDF00"/><circle cx="15" cy="10.5" r="4.4" fill="#002776"/>',
  GB: '<rect width="30" height="21" fill="#012169"/><path d="M0 0l30 21M30 0L0 21" stroke="#fff" stroke-width="4.2"/><path d="M0 0l30 21M30 0L0 21" stroke="#C8102E" stroke-width="2.1"/><path d="M15 0v21M0 10.5h30" stroke="#fff" stroke-width="7"/><path d="M15 0v21M0 10.5h30" stroke="#C8102E" stroke-width="4.2"/>',
  US: '<rect width="30" height="21" fill="#B22234"/><rect y="3.2" width="30" height="3.2" fill="#fff"/><rect y="9.7" width="30" height="3.2" fill="#fff"/><rect y="16.1" width="30" height="3.2" fill="#fff"/><rect width="13" height="11.3" fill="#3C3B6E"/>'
};

// Shown until the hub forwards each node's country — it currently sends "".
const GLOBE =
  '<rect width="30" height="21" rx="2" fill="var(--pill-bg)"/>' +
  '<g fill="none" stroke="var(--text-muted)" stroke-width="1.1" transform="translate(15 10.5)">' +
  '<circle r="6.4"/><ellipse rx="2.6" ry="6.4"/><path d="M-6.4 0h12.8M-5.4 -3.2h10.8M-5.4 3.2h10.8"/></g>';

const NS = "http://www.w3.org/2000/svg";

/** 30x21 flag for an ISO-3166 alpha-2 code; a globe when unknown or absent. */
export function buildFlag(country: string, className = "region-flag"): SVGSVGElement {
  const svg = document.createElementNS(NS, "svg");
  svg.setAttribute("viewBox", "0 0 30 21");
  svg.setAttribute("class", className);
  svg.setAttribute("aria-hidden", "true");
  svg.innerHTML = FLAGS[country.trim().toUpperCase()] ?? GLOBE;
  return svg;
}
