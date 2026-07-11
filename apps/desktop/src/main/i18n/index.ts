// Main-process localisation. The main process is compiled to CommonJS and
// cannot import the renderer's ESM catalogue, so it carries its own small set
// of tray / menu / tooltip strings. The locale is resolved once at startup
// from the persisted preference; changes take effect on next launch.
//
// Non-English values are machine translations pending native review.

export type MainLocale = "en" | "es" | "fr" | "ru" | "uk" | "zh" | "ar" | "fa";

export type MainMessages = Record<
  | "tray.status"
  | "tray.detail"
  | "tray.connect"
  | "tray.disconnect"
  | "tray.show"
  | "tray.hide"
  | "tray.quit"
  | "menu.hideWindow"
  | "menu.quit"
  | "menu.edit"
  | "state.DISCONNECTED"
  | "state.CONNECTING"
  | "state.CONNECTED"
  | "state.DISCONNECTING"
  | "state.ERROR",
  string
>;

const en: MainMessages = {
  "tray.status": "Status: {state}",
  "tray.detail": "Detail: {detail}",
  "tray.connect": "Connect",
  "tray.disconnect": "Disconnect",
  "tray.show": "Show PangeaVPN",
  "tray.hide": "Hide PangeaVPN",
  "tray.quit": "Quit",
  "menu.hideWindow": "Hide Window",
  "menu.quit": "Quit",
  "menu.edit": "Edit",
  "state.DISCONNECTED": "DISCONNECTED",
  "state.CONNECTING": "CONNECTING",
  "state.CONNECTED": "CONNECTED",
  "state.DISCONNECTING": "DISCONNECTING",
  "state.ERROR": "ERROR"
};

const es: MainMessages = {
  "tray.status": "Estado: {state}",
  "tray.detail": "Detalle: {detail}",
  "tray.connect": "Conectar",
  "tray.disconnect": "Desconectar",
  "tray.show": "Mostrar PangeaVPN",
  "tray.hide": "Ocultar PangeaVPN",
  "tray.quit": "Salir",
  "menu.hideWindow": "Ocultar ventana",
  "menu.quit": "Salir",
  "menu.edit": "Editar",
  "state.DISCONNECTED": "DESCONECTADO",
  "state.CONNECTING": "CONECTANDO",
  "state.CONNECTED": "CONECTADO",
  "state.DISCONNECTING": "DESCONECTANDO",
  "state.ERROR": "ERROR"
};

const fr: MainMessages = {
  "tray.status": "État : {state}",
  "tray.detail": "Détail : {detail}",
  "tray.connect": "Se connecter",
  "tray.disconnect": "Se déconnecter",
  "tray.show": "Afficher PangeaVPN",
  "tray.hide": "Masquer PangeaVPN",
  "tray.quit": "Quitter",
  "menu.hideWindow": "Masquer la fenêtre",
  "menu.quit": "Quitter",
  "menu.edit": "Édition",
  "state.DISCONNECTED": "DÉCONNECTÉ",
  "state.CONNECTING": "CONNEXION",
  "state.CONNECTED": "CONNECTÉ",
  "state.DISCONNECTING": "DÉCONNEXION",
  "state.ERROR": "ERREUR"
};

const ru: MainMessages = {
  "tray.status": "Статус: {state}",
  "tray.detail": "Детали: {detail}",
  "tray.connect": "Подключить",
  "tray.disconnect": "Отключить",
  "tray.show": "Показать PangeaVPN",
  "tray.hide": "Скрыть PangeaVPN",
  "tray.quit": "Выход",
  "menu.hideWindow": "Скрыть окно",
  "menu.quit": "Выход",
  "menu.edit": "Правка",
  "state.DISCONNECTED": "ОТКЛЮЧЕНО",
  "state.CONNECTING": "ПОДКЛЮЧЕНИЕ",
  "state.CONNECTED": "ПОДКЛЮЧЕНО",
  "state.DISCONNECTING": "ОТКЛЮЧЕНИЕ",
  "state.ERROR": "ОШИБКА"
};

const uk: MainMessages = {
  "tray.status": "Статус: {state}",
  "tray.detail": "Деталі: {detail}",
  "tray.connect": "Підключити",
  "tray.disconnect": "Відключити",
  "tray.show": "Показати PangeaVPN",
  "tray.hide": "Сховати PangeaVPN",
  "tray.quit": "Вихід",
  "menu.hideWindow": "Сховати вікно",
  "menu.quit": "Вихід",
  "menu.edit": "Редагувати",
  "state.DISCONNECTED": "ВІДКЛЮЧЕНО",
  "state.CONNECTING": "ПІДКЛЮЧЕННЯ",
  "state.CONNECTED": "ПІДКЛЮЧЕНО",
  "state.DISCONNECTING": "ВІДКЛЮЧЕННЯ",
  "state.ERROR": "ПОМИЛКА"
};

const zh: MainMessages = {
  "tray.status": "状态：{state}",
  "tray.detail": "详情：{detail}",
  "tray.connect": "连接",
  "tray.disconnect": "断开",
  "tray.show": "显示 PangeaVPN",
  "tray.hide": "隐藏 PangeaVPN",
  "tray.quit": "退出",
  "menu.hideWindow": "隐藏窗口",
  "menu.quit": "退出",
  "menu.edit": "编辑",
  "state.DISCONNECTED": "已断开",
  "state.CONNECTING": "连接中",
  "state.CONNECTED": "已连接",
  "state.DISCONNECTING": "断开中",
  "state.ERROR": "错误"
};

const ar: MainMessages = {
  "tray.status": "الحالة: {state}",
  "tray.detail": "التفاصيل: {detail}",
  "tray.connect": "اتصال",
  "tray.disconnect": "قطع الاتصال",
  "tray.show": "إظهار PangeaVPN",
  "tray.hide": "إخفاء PangeaVPN",
  "tray.quit": "إنهاء",
  "menu.hideWindow": "إخفاء النافذة",
  "menu.quit": "إنهاء",
  "menu.edit": "تحرير",
  "state.DISCONNECTED": "غير متصل",
  "state.CONNECTING": "جارٍ الاتصال",
  "state.CONNECTED": "متصل",
  "state.DISCONNECTING": "جارٍ قطع الاتصال",
  "state.ERROR": "خطأ"
};

const fa: MainMessages = {
  "tray.status": "وضعیت: {state}",
  "tray.detail": "جزئیات: {detail}",
  "tray.connect": "اتصال",
  "tray.disconnect": "قطع اتصال",
  "tray.show": "نمایش PangeaVPN",
  "tray.hide": "پنهان کردن PangeaVPN",
  "tray.quit": "خروج",
  "menu.hideWindow": "پنهان کردن پنجره",
  "menu.quit": "خروج",
  "menu.edit": "ویرایش",
  "state.DISCONNECTED": "قطع شده",
  "state.CONNECTING": "در حال اتصال",
  "state.CONNECTED": "متصل",
  "state.DISCONNECTING": "در حال قطع اتصال",
  "state.ERROR": "خطا"
};

const CATALOGUES: Record<MainLocale, MainMessages> = { en, es, fr, ru, uk, zh, ar, fa };
const SUPPORTED: readonly MainLocale[] = ["en", "es", "fr", "ru", "uk", "zh", "ar", "fa"];
const DEFAULT: MainLocale = "en";

let active: MainLocale = DEFAULT;

function isMainLocale(value: unknown): value is MainLocale {
  return typeof value === "string" && (SUPPORTED as readonly string[]).includes(value);
}

/** Map an OS language tag (e.g. from app.getLocale()) to a supported locale. */
export function matchMainLocale(tag: string | null | undefined): MainLocale | null {
  if (!tag) return null;
  const primary = tag.toLowerCase().split("-")[0];
  if (isMainLocale(primary)) return primary;
  if (primary === "fas" || primary === "per") return "fa";
  if (primary === "zho" || primary === "chi") return "zh";
  if (primary === "ukr") return "uk";
  return null;
}

/**
 * Resolve the concrete locale from the stored preference and the OS locale.
 * `stored` is "system"/undefined (detect from OS) or a locale code.
 */
export function resolveMainLocale(stored: string | null | undefined, osLocale: string | null | undefined): MainLocale {
  return matchMainLocale(stored) ?? matchMainLocale(osLocale) ?? DEFAULT;
}

export function setMainLocale(locale: MainLocale): void {
  active = CATALOGUES[locale] ? locale : DEFAULT;
}

export function getMainLocale(): MainLocale {
  return active;
}

export function mt(key: keyof MainMessages, params?: Record<string, string>): string {
  const template = CATALOGUES[active][key] ?? en[key];
  if (!params) return template;
  return template.replace(/\{(\w+)\}/g, (m, k: string) =>
    Object.prototype.hasOwnProperty.call(params, k) ? params[k] : m
  );
}

/** Translate a daemon state code (e.g. "CONNECTED") to the active locale. */
export function mtState(state: string): string {
  const key = `state.${state}` as keyof MainMessages;
  return (CATALOGUES[active] as MainMessages)[key] ?? (en as MainMessages)[key] ?? state;
}
