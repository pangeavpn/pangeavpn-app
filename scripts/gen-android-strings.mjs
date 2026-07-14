// Generates apps/android/ui-common/src/main/res/values[-<locale>]/strings.xml
// from the desktop i18n catalogues (apps/desktop/src/renderer/i18n/locales/*.ts).
//
// Only a mobile-relevant subset of desktop keys is carried over; desktop-only
// concepts (tray, updater, logs, verbose debug toggle) are intentionally
// skipped. Mobile-only keys (notifications, kill-switch guide, TV login hint,
// subscription banner, screen titles) have no desktop equivalent and are
// translated here directly for all locales.
import fs from "node:fs";
import path from "node:path";
import process from "node:process";

const rootDir = process.cwd();
const localesDir = path.join(rootDir, "apps", "desktop", "src", "renderer", "i18n", "locales");
const outDir = path.join(rootDir, "apps", "android", "ui-common", "src", "main", "res");

const LOCALES = ["en", "es", "fr", "ru", "uk", "zh", "ar", "fa"];

const RES_DIR = {
  en: "values",
  es: "values-es",
  fr: "values-fr",
  ru: "values-ru",
  uk: "values-uk",
  zh: "values-zh",
  ar: "values-ar",
  fa: "values-fa"
};

// Desktop key -> Android key (dots to underscores, hand-picked mobile subset).
const KEY_MAP = {
  "login.subtitle": "login_subtitle",
  "login.signIn": "login_signin",
  "login.tokenPlaceholder": "login_token_placeholder",
  "hero.connect": "hero_connect",
  "hero.disconnect": "hero_disconnect",
  "hero.selectServer": "hero_select_server",
  "hero.provisioning": "hero_provisioning",
  "hero.noServers": "hero_no_servers",
  "state.DISCONNECTED": "state_disconnected",
  "state.CONNECTING": "state_connecting",
  "state.CONNECTED": "state_connected",
  "state.DISCONNECTING": "state_disconnecting",
  "state.ERROR": "state_error",
  "pill.killSwitch": "pill_killswitch",
  "pill.cloak": "pill_cloak",
  "pill.wireguard": "pill_wireguard",
  "devices.title": "devices_title",
  "devices.remove": "devices_remove",
  "deviceLimit.title": "devicelimit_title",
  "deviceLimit.subtitle": "devicelimit_subtitle"
};

// Mobile-only strings, translated inline for all 8 locales.
const MOBILE_ONLY = {
  notification_connected: {
    en: "Connected", es: "Conectado", fr: "Connecté", ru: "Подключено",
    uk: "Підключено", zh: "已连接", ar: "متصل", fa: "متصل"
  },
  notification_connecting: {
    en: "Connecting…", es: "Conectando…", fr: "Connexion…", ru: "Подключение…",
    uk: "Підключення…", zh: "连接中…", ar: "جارٍ الاتصال...", fa: "در حال اتصال..."
  },
  notification_disconnect: {
    en: "Disconnect", es: "Desconectar", fr: "Se déconnecter", ru: "Отключить",
    uk: "Відключитися", zh: "断开连接", ar: "قطع الاتصال", fa: "قطع اتصال"
  },
  killswitch_title: {
    en: "Kill Switch Active", es: "Kill Switch activo", fr: "Kill Switch actif",
    ru: "Kill Switch включён", uk: "Аварійне вимкнення активне",
    zh: "断网保护已启用", ar: "مفتاح الإيقاف مفعّل", fa: "کلید قطع اضطراری فعال است"
  },
  killswitch_body: {
    en: "Only VPN traffic is allowed until you reconnect or turn off the kill switch.",
    es: "Solo se permite el tráfico de la VPN hasta que vuelvas a conectar o desactives el Kill Switch.",
    fr: "Seul le trafic VPN est autorisé jusqu'à ce que vous vous reconnectiez ou désactiviez le kill switch.",
    ru: "Разрешён только VPN-трафик, пока вы не переподключитесь или не отключите Kill Switch.",
    uk: "Дозволено лише трафік VPN, доки ви не підключитеся знову або не вимкнете аварійне вимкнення.",
    zh: "在重新连接或关闭断网保护之前，仅允许 VPN 流量通过。",
    ar: "يُسمح بحركة VPN فقط حتى تعيد الاتصال أو توقف مفتاح الإيقاف.",
    fa: "تا زمانی که دوباره متصل شوید یا کلید قطع اضطراری را خاموش کنید، فقط ترافیک VPN مجاز است."
  },
  killswitch_open_settings: {
    en: "Open VPN Settings", es: "Abrir ajustes de VPN", fr: "Ouvrir les paramètres VPN",
    ru: "Открыть настройки VPN", uk: "Відкрити налаштування VPN",
    zh: "打开 VPN 设置", ar: "فتح إعدادات VPN", fa: "باز کردن تنظیمات VPN"
  },
  tv_login_hint: {
    en: "Get your token at pangeavpn.org/account",
    es: "Obtén tu token en pangeavpn.org/account",
    fr: "Obtenez votre jeton sur pangeavpn.org/account",
    ru: "Получите токен на pangeavpn.org/account",
    uk: "Отримайте токен на pangeavpn.org/account",
    zh: "在 pangeavpn.org/account 获取您的令牌",
    ar: "احصل على رمزك من pangeavpn.org/account",
    fa: "توکن خود را در pangeavpn.org/account دریافت کنید"
  },
  subscription_expires: {
    en: "Expires {date}", es: "Caduca el {date}", fr: "Expire le {date}",
    ru: "Истекает {date}", uk: "Закінчується {date}",
    zh: "有效期至 {date}", ar: "ينتهي في {date}", fa: "در {date} منقضی می‌شود"
  },
  subscription_none: {
    en: "No active subscription", es: "Sin suscripción activa", fr: "Aucun abonnement actif",
    ru: "Нет активной подписки", uk: "Немає активної підписки",
    zh: "无有效订阅", ar: "لا يوجد اشتراك نشط", fa: "اشتراک فعالی وجود ندارد"
  },
  servers_title: {
    en: "Servers", es: "Servidores", fr: "Serveurs", ru: "Серверы",
    uk: "Сервери", zh: "服务器", ar: "الخوادم", fa: "سرورها"
  },
  settings_title: {
    en: "Settings", es: "Ajustes", fr: "Paramètres", ru: "Настройки",
    uk: "Налаштування", zh: "设置", ar: "الإعدادات", fa: "تنظیمات"
  },
  settings_signout: {
    en: "Sign Out", es: "Cerrar sesión", fr: "Se déconnecter", ru: "Выйти",
    uk: "Вийти", zh: "退出登录", ar: "تسجيل الخروج", fa: "خروج"
  },
  login_title: {
    en: "Sign In", es: "Iniciar sesión", fr: "Se connecter", ru: "Войти",
    uk: "Увійти", zh: "登录", ar: "تسجيل الدخول", fa: "ورود"
  },
  app_name: {
    en: "PangeaVPN", es: "PangeaVPN", fr: "PangeaVPN", ru: "PangeaVPN",
    uk: "PangeaVPN", zh: "PangeaVPN", ar: "PangeaVPN", fa: "PangeaVPN"
  }
};

main();

function main() {
  const parsedLocales = {};
  for (const code of LOCALES) {
    parsedLocales[code] = parseLocale(code);
  }

  let keyCount = 0;
  for (const code of LOCALES) {
    const entries = buildEntries(code, parsedLocales[code]);
    keyCount = Object.keys(entries).length;
    writeStringsXml(code, entries);
  }

  console.log(`Wrote ${LOCALES.length} strings.xml files (${keyCount} keys each) to ${outDir}`);
}

function parseLocale(code) {
  const filePath = path.join(localesDir, `${code}.ts`);
  const source = fs.readFileSync(filePath, "utf8");

  const marker = `export const ${code} = {`;
  const start = source.indexOf(marker);
  if (start === -1) {
    throw new Error(`Could not find "${marker}" in ${filePath}`);
  }

  const objectStart = start + marker.length - 1; // index of the opening "{"
  const objectEnd = findMatchingBrace(source, objectStart);
  if (objectEnd === -1) {
    throw new Error(`Could not find matching closing brace for ${filePath}`);
  }

  const body = source.slice(objectStart + 1, objectEnd);
  const entries = {};
  const pairPattern = /"((?:\\.|[^"\\])*)"\s*:\s*"((?:\\.|[^"\\])*)"/g;
  let match;
  while ((match = pairPattern.exec(body)) !== null) {
    entries[decodeJsString(match[1])] = decodeJsString(match[2]);
  }

  if (Object.keys(entries).length === 0) {
    throw new Error(`Parsed zero key/value pairs from ${filePath}`);
  }

  return entries;
}

function findMatchingBrace(source, openIndex) {
  let depth = 0;
  for (let i = openIndex; i < source.length; i++) {
    if (source[i] === "{") depth++;
    else if (source[i] === "}") {
      depth--;
      if (depth === 0) return i;
    }
  }
  return -1;
}

function decodeJsString(raw) {
  return JSON.parse(`"${raw}"`);
}

function buildEntries(code, localeData) {
  const entries = {};

  for (const [desktopKey, androidKey] of Object.entries(KEY_MAP)) {
    const value = localeData[desktopKey];
    if (value === undefined) {
      throw new Error(`Missing desktop key "${desktopKey}" for locale "${code}"`);
    }
    entries[androidKey] = value;
  }

  for (const [androidKey, translations] of Object.entries(MOBILE_ONLY)) {
    const value = translations[code];
    if (value === undefined) {
      throw new Error(`Missing mobile-only translation "${androidKey}" for locale "${code}"`);
    }
    entries[androidKey] = value;
  }

  return entries;
}

function writeStringsXml(code, entries) {
  const dirPath = path.join(outDir, RES_DIR[code]);
  fs.mkdirSync(dirPath, { recursive: true });

  const lines = Object.keys(entries)
    .sort()
    .map((key) => `    <string name="${key}">${formatValue(entries[key])}</string>`);

  const xml = `<?xml version="1.0" encoding="utf-8"?>\n<resources>\n${lines.join("\n")}\n</resources>\n`;
  fs.writeFileSync(path.join(dirPath, "strings.xml"), xml, "utf8");
}

function formatValue(value) {
  return escapeXml(convertPlaceholders(value));
}

function convertPlaceholders(value) {
  const seen = new Map();
  return value.replace(/\{([a-zA-Z0-9_]+)\}/g, (_, name) => {
    if (!seen.has(name)) {
      seen.set(name, seen.size + 1);
    }
    return `%${seen.get(name)}$s`;
  });
}

function escapeXml(value) {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/'/g, "\\'")
    .replace(/"/g, '\\"');
}
