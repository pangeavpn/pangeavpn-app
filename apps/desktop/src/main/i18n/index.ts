// Separate from the renderer catalogue: CommonJS here can't import its ESM.
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
  | "notify.trayTitle"
  | "notify.trayBody"
  | "notify.menuBarBody"
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
  "notify.trayTitle": "PangeaVPN is still running",
  "notify.trayBody": "It's in your system tray. Click the tray icon to open it again.",
  "notify.menuBarBody": "It's in your menu bar. Click the menu bar icon to open it again.",
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
  "notify.trayTitle": "PangeaVPN sigue en ejecución",
  "notify.trayBody": "Está en la bandeja del sistema. Haz clic en el icono para volver a abrirlo.",
  "notify.menuBarBody": "Está en la barra de menús. Haz clic en el icono para volver a abrirlo.",
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
  "notify.trayTitle": "PangeaVPN est toujours actif",
  "notify.trayBody": "Il est dans la zone de notification. Cliquez sur l'icône pour le rouvrir.",
  "notify.menuBarBody": "Il est dans la barre de menus. Cliquez sur l'icône pour le rouvrir.",
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
  "notify.trayTitle": "PangeaVPN всё ещё работает",
  "notify.trayBody": "Он в системном трее. Нажмите на значок, чтобы открыть его снова.",
  "notify.menuBarBody": "Он в строке меню. Нажмите на значок, чтобы открыть его снова.",
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
  "notify.trayTitle": "PangeaVPN усе ще працює",
  "notify.trayBody": "Він у системному лотку. Натисніть на значок, щоб відкрити його знову.",
  "notify.menuBarBody": "Він у рядку меню. Натисніть на значок, щоб відкрити його знову.",
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
  "notify.trayTitle": "PangeaVPN 仍在运行",
  "notify.trayBody": "它在系统托盘中。点击托盘图标即可重新打开。",
  "notify.menuBarBody": "它在菜单栏中。点击菜单栏图标即可重新打开。",
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
  "notify.trayTitle": "لا يزال PangeaVPN قيد التشغيل",
  "notify.trayBody": "إنه في شريط النظام. انقر على الأيقونة لفتحه مرة أخرى.",
  "notify.menuBarBody": "إنه في شريط القوائم. انقر على الأيقونة لفتحه مرة أخرى.",
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
  "notify.trayTitle": "PangeaVPN همچنان در حال اجراست",
  "notify.trayBody": "در سینی سیستم است. روی نماد کلیک کنید تا دوباره باز شود.",
  "notify.menuBarBody": "در نوار منو است. روی نماد کلیک کنید تا دوباره باز شود.",
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

/** Resolve the locale; `stored` is "system"/undefined (follow the OS) or a code. */
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
