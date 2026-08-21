import type { MessageKey } from "./i18n/messages.js";

/** Same shape as the renderer's `t`, so callers pass it straight through. */
export type Translate = (key: MessageKey, params?: Record<string, string | number>) => string;

export const HUB_METHOD_TITLE_KEYS: Record<HubMethodName, MessageKey> = {
  directIp: "settings.provisioning.directIp.title",
  shadowsocks: "settings.provisioning.hubShadowsocks.title",
  fronted: "settings.provisioning.hubFronted.title",
  normal: "settings.provisioning.hubNormal.title"
};

const UNAVAILABLE_KEYS: Record<NonNullable<HubMethodTestResult["unavailable"]>, MessageKey> = {
  noAddress: "settings.provisioning.result.noAddress",
  noCredentials: "settings.provisioning.result.noCredentials",
  noRelay: "settings.provisioning.result.noRelay",
  busy: "settings.provisioning.result.busy"
};

/** Names the method carrying hub traffic, with the address it won on when the
 *  main process reported one. */
export function hubActiveText(status: HubStatus, t: Translate): string {
  if (!status.active) {
    return t("settings.provisioning.active.none");
  }
  const method = t(HUB_METHOD_TITLE_KEYS[status.active]);
  return status.detail
    ? t("settings.provisioning.active.detail", { method, detail: status.detail })
    : t("settings.provisioning.active.using", { method });
}

/** A method that had nothing to try reports why, rather than reading as a
 *  failure of the network. */
export function hubTestText(result: HubMethodTestResult, t: Translate): string {
  if (result.unavailable) {
    return t(UNAVAILABLE_KEYS[result.unavailable]);
  }
  if (!result.ok) {
    return t("settings.provisioning.result.fail");
  }
  return result.detail
    ? t("settings.provisioning.result.okDetail", { detail: result.detail, ms: result.ms })
    : t("settings.provisioning.result.ok", { ms: result.ms });
}
