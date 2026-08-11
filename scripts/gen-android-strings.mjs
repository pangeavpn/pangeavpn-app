// Generates the Android strings.xml files from the desktop i18n catalogues.
// KEY_MAP carries the mobile-relevant subset; MOBILE_ONLY has no desktop twin.
import fs from "node:fs";
import path from "node:path";
import process from "node:process";

const rootDir = process.cwd();
const localesDir = path.join(rootDir, "apps", "desktop", "src", "renderer", "i18n", "locales");
const outDir = path.join(rootDir, "apps", "android", "ui-common", "src", "main", "res");
// :core cannot depend on :ui-common (ui-common already depends on core), so
// the strings the VpnService itself needs are emitted into core.
const coreOutDir = path.join(rootDir, "apps", "android", "core", "src", "main", "res");
// R classes are non-transitive, so a key must live in the module that uses it.
const CORE_KEYS = new Set([
  "notification_connected",
  "notification_connecting",
  "notification_disconnecting",
  "notification_disconnected",
  "notification_error",
  "notification_disconnect",
  "notification_channel_name"
]);

// Notification titles reuse the state wording rather than translating it twice.
const CORE_ALIASES = {
  notification_disconnecting: "state_disconnecting",
  notification_disconnected: "state_disconnected",
  notification_error: "state_error"
};

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
  "hero.disconnecting": "hero_disconnecting",
  "hero.noServers": "hero_no_servers",
  "hero.region": "hero_region",
  "hero.allRegions": "hero_all_regions",
  "hero.refreshServers": "hero_refresh_servers",
  "hero.headline.DISCONNECTED": "hero_headline_disconnected",
  "hero.headline.CONNECTING": "hero_headline_connecting",
  "hero.headline.CONNECTED": "hero_headline_connected",
  "hero.headline.DISCONNECTING": "hero_headline_disconnecting",
  "hero.headline.ERROR": "hero_headline_error",
  "hero.emphasis.DISCONNECTED": "hero_emphasis_disconnected",
  "hero.emphasis.CONNECTING": "hero_emphasis_connecting",
  "hero.emphasis.CONNECTED": "hero_emphasis_connected",
  "hero.emphasis.DISCONNECTING": "hero_emphasis_disconnecting",
  "hero.emphasis.ERROR": "hero_emphasis_error",
  "fact.session": "fact_session",
  "fact.down": "fact_down",
  "fact.up": "fact_up",
  "fact.via": "fact_via",
  "region.more": "region_more",
  "region.bestOf": "region_best_of",
  "region.autoOf": "region_auto_of",
  "region.pinned": "region_pinned",
  "header.menu": "header_menu",
  "header.toggleTheme": "header_toggle_theme",
  "state.DISCONNECTED": "state_disconnected",
  "state.CONNECTING": "state_connecting",
  "state.CONNECTED": "state_connected",
  "state.DISCONNECTING": "state_disconnecting",
  "state.ERROR": "state_error",
  "status.transport.cloak": "transport_status_cloak",
  "status.transport.reality": "transport_status_reality",
  "status.transport.shadowsocks": "transport_status_shadowsocks",
  "status.transport.hysteria2": "transport_status_hysteria2",
  "status.transport.naive": "transport_status_naive",
  "status.transport.snowflake": "transport_status_snowflake",
  "status.transport.none": "transport_status_none",
  "settings.transport.heading": "settings_transport_heading",
  "settings.transport.description": "settings_transport_description",
  "settings.transport.auto": "settings_transport_auto",
  "settings.transport.cloak": "settings_transport_cloak",
  "settings.transport.reality": "settings_transport_reality",
  "settings.transport.shadowsocks": "settings_transport_shadowsocks",
  "settings.transport.hysteria2": "settings_transport_hysteria2",
  "settings.transport.snowflake": "settings_transport_snowflake",
  "serverPicker.noServersForTransport": "server_picker_none_for_transport",
  "settings.censorship.heading": "settings_censorship_heading",
  "settings.censorship.description": "settings_censorship_description",
  "settings.censorship.directIp.title": "settings_censorship_directip_title",
  "settings.censorship.directIp.hint": "settings_censorship_directip_hint",
  "settings.censorship.hubShadowsocks.title": "settings_censorship_shadowsocks_title",
  "settings.censorship.hubShadowsocks.hint": "settings_censorship_shadowsocks_hint",
  "settings.censorship.hubFronted.title": "settings_censorship_fronted_title",
  "settings.censorship.hubFronted.hint": "settings_censorship_fronted_hint",
  "settings.censorship.hubNormal.title": "settings_censorship_normal_title",
  "settings.censorship.hubNormal.hint": "settings_censorship_normal_hint",
  "settings.censorship.lastMethod": "settings_censorship_last_method",
  "settings.network.heading": "settings_network_heading",
  "settings.network.description": "settings_network_description",
  "settings.network.allowLan.title": "settings_network_allowlan_title",
  "settings.network.allowLan.hint": "settings_network_allowlan_hint",
  "settings.network.dns.title": "settings_network_dns_title",
  "settings.network.dns.placeholder": "settings_network_dns_placeholder",
  "settings.network.dns.hint": "settings_network_dns_hint",
  "settings.network.dns.invalid": "settings_network_dns_invalid",
  "settings.network.mtu.title": "settings_network_mtu_title",
  "settings.network.mtu.hint": "settings_network_mtu_hint",
  "devices.title": "devices_title",
  "devices.remove": "devices_remove",
  "deviceLimit.title": "devicelimit_title",
  "deviceLimit.subtitle": "devicelimit_subtitle"
};

// Mobile-only strings, translated inline for all 8 locales.
const MOBILE_ONLY = {
  toggle_on: {
    en: "On", es: "Activado", fr: "Activé", ru: "Вкл",
    uk: "Увімк", zh: "开启", ar: "مفعّل", fa: "روشن"
  },
  toggle_off: {
    en: "Off", es: "Desactivado", fr: "Désactivé", ru: "Выкл",
    uk: "Вимк", zh: "关闭", ar: "معطّل", fa: "خاموش"
  },
  split_tunnel_title: {
    en: "Split Tunnelling", es: "Túnel dividido", fr: "Tunnel divisé",
    ru: "Раздельное туннелирование", uk: "Роздільне тунелювання",
    zh: "分应用代理", ar: "الأنفاق المقسّمة", fa: "تونل تفکیک‌شده"
  },
  split_tunnel_hint: {
    en: "Apps switched on here bypass the VPN and use your normal connection.",
    es: "Las apps activadas aquí omiten la VPN y usan tu conexión normal.",
    fr: "Les applications activées ici contournent le VPN et utilisent votre connexion normale.",
    ru: "Приложения, включённые здесь, идут в обход VPN через обычное соединение.",
    uk: "Застосунки, увімкнені тут, обходять VPN і використовують звичайне з'єднання.",
    zh: "在此开启的应用将绕过 VPN，使用您的普通网络连接。",
    ar: "التطبيقات المفعّلة هنا تتجاوز الشبكة الافتراضية وتستخدم اتصالك العادي.",
    fa: "برنامه‌های فعال‌شده در اینجا از VPN عبور نمی‌کنند و از اتصال عادی شما استفاده می‌کنند."
  },
  settings_save: {
    en: "Save", es: "Guardar", fr: "Enregistrer", ru: "Сохранить",
    uk: "Зберегти", zh: "保存", ar: "حفظ", fa: "ذخیره"
  },
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
  notification_channel_name: {
    en: "VPN status", es: "Estado de la VPN", fr: "État du VPN",
    ru: "Состояние VPN", uk: "Стан VPN",
    zh: "VPN 状态", ar: "حالة VPN", fa: "وضعیت VPN"
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
  devicelimit_back: {
    en: "Back", es: "Atrás", fr: "Retour", ru: "Назад",
    uk: "Назад", zh: "返回", ar: "رجوع", fa: "بازگشت"
  },
  login_account_label: {
    en: "Account number", es: "Número de cuenta", fr: "Numéro de compte",
    ru: "Номер аккаунта", uk: "Номер акаунта",
    zh: "账号", ar: "رقم الحساب", fa: "شماره حساب"
  },
  login_hint: {
    en: "Find your account number at pangeavpn.org/account",
    es: "Encuentra tu número de cuenta en pangeavpn.org/account",
    fr: "Trouvez votre numéro de compte sur pangeavpn.org/account",
    ru: "Номер аккаунта — на pangeavpn.org/account",
    uk: "Номер акаунта — на pangeavpn.org/account",
    zh: "在 pangeavpn.org/account 查找您的账号",
    ar: "اعثر على رقم حسابك في pangeavpn.org/account",
    fa: "شماره حساب خود را در pangeavpn.org/account پیدا کنید"
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

  let uiCount = 0;
  let coreCount = 0;
  for (const code of LOCALES) {
    const entries = buildEntries(code, parsedLocales[code]);
    for (const [alias, source] of Object.entries(CORE_ALIASES)) {
      if (entries[source] === undefined) {
        throw new Error(`Alias "${alias}" needs missing key "${source}" for locale "${code}"`);
      }
      entries[alias] = entries[source];
    }
    const coreEntries = {};
    const uiEntries = {};
    for (const [key, value] of Object.entries(entries)) {
      (CORE_KEYS.has(key) ? coreEntries : uiEntries)[key] = value;
    }
    uiCount = Object.keys(uiEntries).length;
    coreCount = Object.keys(coreEntries).length;
    writeStringsXml(outDir, code, uiEntries);
    writeStringsXml(coreOutDir, code, coreEntries);
  }

  console.log(`Wrote ${LOCALES.length} ui-common files (${uiCount} keys) and ${LOCALES.length} core files (${coreCount} keys)`);
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

function writeStringsXml(root, code, entries) {
  const dirPath = path.join(root, RES_DIR[code]);
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
    .replace(/\\/g, "\\\\")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/'/g, "\\'")
    .replace(/"/g, '\\"');
}
