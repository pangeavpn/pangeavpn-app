export const DNS_PRESETS = {
  adguard: ["94.140.14.14", "94.140.15.15"],
  cloudflare: ["1.1.1.1", "1.0.0.1"],
  quad9: ["9.9.9.9", "149.112.112.112"],
  cloudflareFamily: ["1.1.1.3", "1.0.0.3"],
  google: ["8.8.8.8", "8.8.4.4"]
} as const;

export type DnsPreset = keyof typeof DNS_PRESETS;
export type DnsChoice = "automatic" | DnsPreset | "custom";

export function dnsChoiceFor(servers: readonly string[]): DnsChoice {
  if (servers.length === 0) return "automatic";
  for (const [name, preset] of Object.entries(DNS_PRESETS) as [DnsPreset, readonly string[]][]) {
    if (servers.length === preset.length && servers.every((server, index) => server === preset[index])) {
      return name;
    }
  }
  return "custom";
}

/** Custom has no fixed addresses; automatic deliberately resolves to an empty override. */
export function dnsServersFor(choice: DnsChoice): string[] | null {
  if (choice === "automatic") return [];
  if (choice === "custom") return null;
  return [...DNS_PRESETS[choice]];
}
