// Separate from the renderer catalogue: CommonJS here can't import its ESM
// at runtime, but a type-only import is erased and stays a compile-time link.
// Non-English values are machine translations pending native review.
import type { Locale } from "../../renderer/i18n/messages.js";

export type MainLocale = Locale;

export type MainMessages = Record<
  | "tray.status"
  | "tray.detail"
  | "tray.blocked"
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
  | "notify.connectedTitle"
  | "notify.connectedBody"
  | "notify.reconnectingTitle"
  | "notify.reconnectingBody"
  | "notify.restoredTitle"
  | "notify.restoredBody"
  | "notify.disconnectedTitle"
  | "notify.disconnectedBody"
  | "notify.blockingTitle"
  | "notify.blockingBody"
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
  "tray.blocked": "Kill switch is on — use Disconnect for internet without VPN",
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
  "notify.connectedTitle": "Connected",
  "notify.connectedBody": "Your traffic is now protected.",
  "notify.reconnectingTitle": "Connection lost",
  "notify.reconnectingBody": "PangeaVPN is reconnecting…",
  "notify.restoredTitle": "Back online",
  "notify.restoredBody": "Your VPN connection is restored.",
  "notify.disconnectedTitle": "Disconnected",
  "notify.disconnectedBody": "The VPN is off.",
  "notify.blockingTitle": "Internet paused to protect you",
  "notify.blockingBody": "The VPN was interrupted, so the kill switch is blocking traffic while PangeaVPN reconnects. Open the app and press Disconnect to go online without VPN.",
  "state.DISCONNECTED": "DISCONNECTED",
  "state.CONNECTING": "CONNECTING",
  "state.CONNECTED": "CONNECTED",
  "state.DISCONNECTING": "DISCONNECTING",
  "state.ERROR": "ERROR"
};

const es: MainMessages = {
  "tray.status": "Estado: {state}",
  "tray.detail": "Detalle: {detail}",
  "tray.blocked": "Kill switch activo: usa Desconectar para tener internet sin VPN",
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
  "notify.connectedTitle": "Conectado",
  "notify.connectedBody": "Tu tráfico ahora está protegido.",
  "notify.reconnectingTitle": "Conexión perdida",
  "notify.reconnectingBody": "PangeaVPN se está reconectando…",
  "notify.restoredTitle": "De nuevo en línea",
  "notify.restoredBody": "La conexión VPN se ha restablecido.",
  "notify.disconnectedTitle": "Desconectado",
  "notify.disconnectedBody": "La VPN está apagada.",
  "notify.blockingTitle": "Internet en pausa para protegerte",
  "notify.blockingBody": "La VPN se interrumpió, así que el kill switch está bloqueando el tráfico mientras PangeaVPN se reconecta. Abre la app y pulsa Desconectar para navegar sin VPN.",
  "state.DISCONNECTED": "DESCONECTADO",
  "state.CONNECTING": "CONECTANDO",
  "state.CONNECTED": "CONECTADO",
  "state.DISCONNECTING": "DESCONECTANDO",
  "state.ERROR": "ERROR"
};

const fr: MainMessages = {
  "tray.status": "État : {state}",
  "tray.detail": "Détail : {detail}",
  "tray.blocked": "Kill switch actif : utilisez Se déconnecter pour avoir Internet sans VPN",
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
  "notify.connectedTitle": "Connecté",
  "notify.connectedBody": "Votre trafic est désormais protégé.",
  "notify.reconnectingTitle": "Connexion perdue",
  "notify.reconnectingBody": "PangeaVPN se reconnecte…",
  "notify.restoredTitle": "De retour en ligne",
  "notify.restoredBody": "La connexion VPN est rétablie.",
  "notify.disconnectedTitle": "Déconnecté",
  "notify.disconnectedBody": "Le VPN est désactivé.",
  "notify.blockingTitle": "Internet en pause pour vous protéger",
  "notify.blockingBody": "Le VPN a été interrompu : le kill switch bloque le trafic pendant que PangeaVPN se reconnecte. Ouvrez l'app et appuyez sur Se déconnecter pour naviguer sans VPN.",
  "state.DISCONNECTED": "DÉCONNECTÉ",
  "state.CONNECTING": "CONNEXION",
  "state.CONNECTED": "CONNECTÉ",
  "state.DISCONNECTING": "DÉCONNEXION",
  "state.ERROR": "ERREUR"
};

const ru: MainMessages = {
  "tray.status": "Статус: {state}",
  "tray.detail": "Детали: {detail}",
  "tray.blocked": "Kill switch включён — нажмите «Отключить» для интернета без VPN",
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
  "notify.connectedTitle": "Подключено",
  "notify.connectedBody": "Ваш трафик теперь защищён.",
  "notify.reconnectingTitle": "Соединение потеряно",
  "notify.reconnectingBody": "PangeaVPN переподключается…",
  "notify.restoredTitle": "Снова в сети",
  "notify.restoredBody": "VPN-соединение восстановлено.",
  "notify.disconnectedTitle": "Отключено",
  "notify.disconnectedBody": "VPN выключен.",
  "notify.blockingTitle": "Интернет приостановлен для вашей защиты",
  "notify.blockingBody": "VPN был прерван, поэтому kill switch блокирует трафик, пока PangeaVPN переподключается. Откройте приложение и нажмите «Отключить», чтобы выйти в сеть без VPN.",
  "state.DISCONNECTED": "ОТКЛЮЧЕНО",
  "state.CONNECTING": "ПОДКЛЮЧЕНИЕ",
  "state.CONNECTED": "ПОДКЛЮЧЕНО",
  "state.DISCONNECTING": "ОТКЛЮЧЕНИЕ",
  "state.ERROR": "ОШИБКА"
};

const uk: MainMessages = {
  "tray.status": "Статус: {state}",
  "tray.detail": "Деталі: {detail}",
  "tray.blocked": "Kill switch увімкнено — натисніть «Відключити» для інтернету без VPN",
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
  "notify.connectedTitle": "Підключено",
  "notify.connectedBody": "Ваш трафік тепер захищено.",
  "notify.reconnectingTitle": "З'єднання втрачено",
  "notify.reconnectingBody": "PangeaVPN перепідключається…",
  "notify.restoredTitle": "Знову в мережі",
  "notify.restoredBody": "VPN-з'єднання відновлено.",
  "notify.disconnectedTitle": "Відключено",
  "notify.disconnectedBody": "VPN вимкнено.",
  "notify.blockingTitle": "Інтернет призупинено для вашого захисту",
  "notify.blockingBody": "VPN було перервано, тому kill switch блокує трафік, поки PangeaVPN перепідключається. Відкрийте застосунок і натисніть «Відключити», щоб вийти в мережу без VPN.",
  "state.DISCONNECTED": "ВІДКЛЮЧЕНО",
  "state.CONNECTING": "ПІДКЛЮЧЕННЯ",
  "state.CONNECTED": "ПІДКЛЮЧЕНО",
  "state.DISCONNECTING": "ВІДКЛЮЧЕННЯ",
  "state.ERROR": "ПОМИЛКА"
};

const zh: MainMessages = {
  "tray.status": "状态：{state}",
  "tray.detail": "详情：{detail}",
  "tray.blocked": "终止开关已启用——点击「断开」即可在无 VPN 时上网",
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
  "notify.connectedTitle": "已连接",
  "notify.connectedBody": "您的流量现已受到保护。",
  "notify.reconnectingTitle": "连接已断开",
  "notify.reconnectingBody": "PangeaVPN 正在重新连接…",
  "notify.restoredTitle": "已恢复在线",
  "notify.restoredBody": "VPN 连接已恢复。",
  "notify.disconnectedTitle": "已断开",
  "notify.disconnectedBody": "VPN 已关闭。",
  "notify.blockingTitle": "已暂停网络以保护您",
  "notify.blockingBody": "VPN 连接被中断，终止开关正在拦截流量，PangeaVPN 正在重新连接。打开应用并点击「断开」即可在无 VPN 的情况下上网。",
  "state.DISCONNECTED": "已断开",
  "state.CONNECTING": "连接中",
  "state.CONNECTED": "已连接",
  "state.DISCONNECTING": "断开中",
  "state.ERROR": "错误"
};

const ar: MainMessages = {
  "tray.status": "الحالة: {state}",
  "tray.detail": "التفاصيل: {detail}",
  "tray.blocked": "مفتاح الإيقاف مفعّل — استخدم قطع الاتصال للإنترنت بدون VPN",
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
  "notify.connectedTitle": "متصل",
  "notify.connectedBody": "حركة بياناتك محمية الآن.",
  "notify.reconnectingTitle": "انقطع الاتصال",
  "notify.reconnectingBody": "يعيد PangeaVPN الاتصال…",
  "notify.restoredTitle": "عاد الاتصال",
  "notify.restoredBody": "تمت استعادة اتصال VPN.",
  "notify.disconnectedTitle": "غير متصل",
  "notify.disconnectedBody": "تم إيقاف VPN.",
  "notify.blockingTitle": "تم إيقاف الإنترنت مؤقتًا لحمايتك",
  "notify.blockingBody": "انقطع اتصال VPN، لذا يحجب مفتاح الإيقاف حركة البيانات بينما يعيد PangeaVPN الاتصال. افتح التطبيق واضغط على قطع الاتصال للاتصال بالإنترنت بدون VPN.",
  "state.DISCONNECTED": "غير متصل",
  "state.CONNECTING": "جارٍ الاتصال",
  "state.CONNECTED": "متصل",
  "state.DISCONNECTING": "جارٍ قطع الاتصال",
  "state.ERROR": "خطأ"
};

const fa: MainMessages = {
  "tray.status": "وضعیت: {state}",
  "tray.detail": "جزئیات: {detail}",
  "tray.blocked": "کلید قطع اضطراری فعال است — برای اینترنت بدون VPN «قطع اتصال» را بزنید",
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
  "notify.connectedTitle": "متصل شد",
  "notify.connectedBody": "ترافیک شما اکنون محافظت می‌شود.",
  "notify.reconnectingTitle": "اتصال قطع شد",
  "notify.reconnectingBody": "PangeaVPN در حال اتصال مجدد است…",
  "notify.restoredTitle": "دوباره آنلاین",
  "notify.restoredBody": "اتصال VPN بازیابی شد.",
  "notify.disconnectedTitle": "قطع شد",
  "notify.disconnectedBody": "VPN خاموش است.",
  "notify.blockingTitle": "اینترنت برای محافظت از شما موقتاً متوقف شد",
  "notify.blockingBody": "اتصال VPN قطع شد، بنابراین کلید قطع اضطراری ترافیک را مسدود می‌کند تا PangeaVPN دوباره متصل شود. برنامه را باز کنید و «قطع اتصال» را بزنید تا بدون VPN آنلاین شوید.",
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
