// Converts the generated Android strings.xml files into iOS Localizable.strings.
// IOS_ONLY holds the keys that have no Android twin.
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const ANDROID_MODULES = ["ui-common", "core"];

const LOCALES = ["en", "es", "fr", "ru", "uk", "zh", "ar", "fa"];

const ANDROID_DIR = {
  en: "values", es: "values-es", fr: "values-fr", ru: "values-ru",
  uk: "values-uk", zh: "values-zh", ar: "values-ar", fa: "values-fa"
};

const LPROJ_DIR = {
  en: "en", es: "es", fr: "fr", ru: "ru", uk: "uk", zh: "zh-Hans", ar: "ar", fa: "fa"
};

// English must match the defaultValue used in the Swift call sites.
const IOS_ONLY = {
  settings_kill_switch: {
    en: "Kill switch", es: "Interruptor de seguridad", fr: "Coupure de sécurité", ru: "Аварийное отключение",
    uk: "Аварійне відключення", zh: "断网保护", ar: "مفتاح الإيقاف", fa: "کلید قطع اضطراری"
  },
  settings_kill_switch_hint: {
    en: "Block all traffic outside the tunnel. Takes effect on the next connect.",
    es: "Bloquea todo el tráfico fuera del túnel. Se aplica en la próxima conexión.",
    fr: "Bloque tout le trafic hors du tunnel. Prend effet à la prochaine connexion.",
    ru: "Блокирует весь трафик вне туннеля. Вступает в силу при следующем подключении.",
    uk: "Блокує весь трафік поза тунелем. Набуває чинності під час наступного підключення.",
    zh: "阻止隧道之外的所有流量。下次连接时生效。",
    ar: "يحظر كل حركة المرور خارج النفق. يسري عند الاتصال التالي.",
    fa: "تمام ترافیک خارج از تونل را مسدود می‌کند. در اتصال بعدی اعمال می‌شود."
  },
  error_no_server: {
    en: "Open PangeaVPN and pick a region.",
    es: "Abre PangeaVPN y elige una región.",
    fr: "Ouvrez PangeaVPN et choisissez une région.",
    ru: "Откройте PangeaVPN и выберите регион.",
    uk: "Відкрийте PangeaVPN і виберіть регіон.",
    zh: "请打开 PangeaVPN 并选择一个地区。",
    ar: "افتح PangeaVPN واختر منطقة.",
    fa: "PangeaVPN را باز کنید و یک منطقه انتخاب کنید."
  },
  error_signed_out: {
    en: "Open PangeaVPN to sign in.",
    es: "Abre PangeaVPN para iniciar sesión.",
    fr: "Ouvrez PangeaVPN pour vous connecter.",
    ru: "Откройте PangeaVPN, чтобы войти.",
    uk: "Відкрийте PangeaVPN, щоб увійти.",
    zh: "请打开 PangeaVPN 登录。",
    ar: "افتح PangeaVPN لتسجيل الدخول.",
    fa: "برای ورود، PangeaVPN را باز کنید."
  },
  error_no_tun: {
    en: "Could not open the tunnel interface",
    es: "No se pudo abrir la interfaz del túnel",
    fr: "Impossible d'ouvrir l'interface du tunnel",
    ru: "Не удалось открыть интерфейс туннеля",
    uk: "Не вдалося відкрити інтерфейс тунелю",
    zh: "无法打开隧道接口",
    ar: "تعذّر فتح واجهة النفق",
    fa: "باز کردن رابط تونل ممکن نشد"
  },
  error_reconnect_failed: {
    en: "Could not reconnect after the network changed",
    es: "No se pudo volver a conectar tras el cambio de red",
    fr: "Impossible de se reconnecter après le changement de réseau",
    ru: "Не удалось переподключиться после смены сети",
    uk: "Не вдалося перепідключитися після зміни мережі",
    zh: "网络变化后无法重新连接",
    ar: "تعذّرت إعادة الاتصال بعد تغيّر الشبكة",
    fa: "پس از تغییر شبکه، اتصال دوباره برقرار نشد"
  },
  error_stopped: {
    en: "The connection was stopped",
    es: "La conexión se detuvo",
    fr: "La connexion a été interrompue",
    ru: "Соединение остановлено",
    uk: "З'єднання зупинено",
    zh: "连接已停止",
    ar: "تم إيقاف الاتصال",
    fa: "اتصال متوقف شد"
  },
  settings_done: {
    en: "Done", es: "Listo", fr: "Terminé", ru: "Готово",
    uk: "Готово", zh: "完成", ar: "تم", fa: "تمام"
  },
  region_refresh: {
    en: "Refresh regions", es: "Actualizar regiones", fr: "Actualiser les régions", ru: "Обновить регионы",
    uk: "Оновити регіони", zh: "刷新地区", ar: "تحديث المناطق", fa: "به‌روزرسانی مناطق"
  },
  error_device_locked: {
    en: "Unlock your iPhone to connect.",
    es: "Desbloquea tu iPhone para conectarte.",
    fr: "Déverrouillez votre iPhone pour vous connecter.",
    ru: "Разблокируйте iPhone, чтобы подключиться.",
    uk: "Розблокуйте iPhone, щоб підключитися.",
    zh: "请解锁 iPhone 后再连接。",
    ar: "افتح قفل iPhone للاتصال.",
    fa: "برای اتصال، قفل iPhone را باز کنید."
  },
  error_subscription_expired: {
    en: "Your subscription has expired.",
    es: "Tu suscripción ha caducado.",
    fr: "Votre abonnement a expiré.",
    ru: "Срок действия вашей подписки истёк.",
    uk: "Термін дії вашої підписки минув.",
    zh: "您的订阅已过期。",
    ar: "انتهت صلاحية اشتراكك.",
    fa: "اشتراک شما منقضی شده است."
  },
  error_connect_failed: {
    en: "Could not connect. Check your connection or try another region.",
    es: "No se pudo conectar. Comprueba tu conexión o prueba otra región.",
    fr: "Connexion impossible. Vérifiez votre connexion ou essayez une autre région.",
    ru: "Не удалось подключиться. Проверьте соединение или выберите другой регион.",
    uk: "Не вдалося підключитися. Перевірте з'єднання або виберіть інший регіон.",
    zh: "无法连接。请检查网络连接或尝试其他地区。",
    ar: "تعذّر الاتصال. تحقق من اتصالك أو جرّب منطقة أخرى.",
    fa: "اتصال برقرار نشد. اتصال خود را بررسی کنید یا منطقه دیگری را امتحان کنید."
  },
  error_dns_invalid: {
    en: "No usable DNS server. Check Custom DNS in Settings.",
    es: "No hay ningún servidor DNS utilizable. Revisa el DNS personalizado en Ajustes.",
    fr: "Aucun serveur DNS utilisable. Vérifiez le DNS personnalisé dans Paramètres.",
    ru: "Нет доступного DNS-сервера. Проверьте пользовательский DNS в настройках.",
    uk: "Немає придатного DNS-сервера. Перевірте власний DNS у налаштуваннях.",
    zh: "没有可用的 DNS 服务器。请在设置中检查自定义 DNS。",
    ar: "لا يوجد خادم DNS صالح. تحقق من DNS المخصص في الإعدادات.",
    fa: "هیچ سرور DNS قابل استفاده‌ای وجود ندارد. DNS سفارشی را در تنظیمات بررسی کنید."
  },
  error_signin_not_saved: {
    en: "Could not save your sign-in on this device.",
    es: "No se pudo guardar tu inicio de sesión en este dispositivo.",
    fr: "Impossible d'enregistrer votre connexion sur cet appareil.",
    ru: "Не удалось сохранить вход на этом устройстве.",
    uk: "Не вдалося зберегти вхід на цьому пристрої.",
    zh: "无法在此设备上保存您的登录信息。",
    ar: "تعذّر حفظ تسجيل الدخول على هذا الجهاز.",
    fa: "ذخیرهٔ ورود شما در این دستگاه ممکن نشد."
  },
  login_empty_account: {
    en: "Enter your account number",
    es: "Introduce tu número de cuenta",
    fr: "Saisissez votre numéro de compte",
    ru: "Введите номер аккаунта",
    uk: "Введіть номер облікового запису",
    zh: "请输入您的账号",
    ar: "أدخل رقم حسابك",
    fa: "شماره حساب خود را وارد کنید"
  }
};

const ANDROID_ESCAPES = { n: "\n", t: "\t" };
const XML_ENTITIES = { amp: "&", lt: "<", gt: ">", quot: '"', apos: "'" };

const PLURAL_PREFIX = "@plural:";
const PLURAL_CATEGORIES = ["zero", "one", "two", "few", "many", "other"];

function unescapeAndroid(xmlValue) {
  return xmlValue
    .replace(/&(amp|lt|gt|quot|apos);/g, (_, name) => XML_ENTITIES[name])
    .replace(/\\(.)/gs, (_, ch) => ANDROID_ESCAPES[ch] ?? ch);
}

function toStringsValue(xmlValue) {
  return unescapeAndroid(xmlValue)
    .replace(/%(\d+)\$s/g, "%$1$$@")
    .replace(/\\/g, "\\\\")
    .replace(/"/g, '\\"')
    .replace(/\n/g, "\\n")
    .replace(/\t/g, "\\t");
}

function isPlural(xmlValue) {
  return xmlValue.startsWith(PLURAL_PREFIX);
}

function parsePlural(xmlValue) {
  const forms = {};
  for (const part of xmlValue.slice(PLURAL_PREFIX.length).split("|")) {
    const eq = part.indexOf("=");
    const category = part.slice(0, eq);
    if (eq < 0 || !PLURAL_CATEGORIES.includes(category)) {
      throw new Error(`Bad plural segment "${part}"`);
    }
    forms[category] = unescapeAndroid(part.slice(eq + 1)).replace(/%\d+\$[sd]/g, "%d");
  }
  if (!("other" in forms)) throw new Error(`Plural without "other": ${xmlValue}`);
  return forms;
}

function parseAndroid(xml, file) {
  const entries = {};
  for (const m of xml.matchAll(/<string name="([^"]+)"[^>]*>(.*?)<\/string>/gs)) {
    if (m[1] in entries) throw new Error(`Duplicate key "${m[1]}" in ${file}`);
    entries[m[1]] = m[2];
  }
  return entries;
}

function collectRaw(rootDir, iosOnly) {
  const raw = {};
  for (const code of LOCALES) {
    const merged = {};
    for (const mod of ANDROID_MODULES) {
      const file = path.join(rootDir, "apps", "android", mod, "src", "main", "res", ANDROID_DIR[code], "strings.xml");
      for (const [key, value] of Object.entries(parseAndroid(fs.readFileSync(file, "utf8"), file))) {
        if (key in merged) throw new Error(`Key "${key}" appears in more than one module for ${code}`);
        merged[key] = value;
      }
    }
    for (const [key, translations] of Object.entries(iosOnly)) {
      if (key in merged) throw new Error(`IOS_ONLY key "${key}" collides with an Android key`);
      if (translations[code] === undefined) throw new Error(`Missing iOS-only translation "${key}" for "${code}"`);
      merged[key] = translations[code];
    }
    raw[code] = merged;
  }
  return raw;
}

function sortKeys(obj) {
  return Object.fromEntries(Object.entries(obj).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)));
}

function buildAll(rootDir, iosOnly = IOS_ONLY) {
  const raw = collectRaw(rootDir, iosOnly);
  const pluralKeys = new Set();
  for (const code of LOCALES) {
    for (const [key, value] of Object.entries(raw[code])) if (isPlural(value)) pluralKeys.add(key);
  }
  const tables = {};
  const plurals = {};
  for (const code of LOCALES) {
    const strings = {};
    const forms = {};
    for (const [key, value] of Object.entries(raw[code])) {
      if (pluralKeys.has(key)) {
        forms[key] = isPlural(value) ? parsePlural(value) : { other: unescapeAndroid(value).replace(/%\d+\$[sd]/g, "%d") };
      } else {
        strings[key] = toStringsValue(value);
      }
    }
    tables[code] = sortKeys(strings);
    plurals[code] = sortKeys(forms);
  }
  return { tables, plurals };
}

function buildTables(rootDir, iosOnly = IOS_ONLY) {
  return buildAll(rootDir, iosOnly).tables;
}

function render(table) {
  return Object.entries(table).map(([k, v]) => `"${k}" = "${v}";
`).join("");
}

function xmlEscape(text) {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function renderStringsdict(plurals) {
  const entries = Object.entries(plurals).map(([key, forms]) => {
    const cats = PLURAL_CATEGORIES.filter((c) => c in forms)
      .map((c) => `\t\t\t<key>${c}</key>\n\t\t\t<string>${xmlEscape(forms[c])}</string>\n`).join("");
    return [
      `\t<key>${xmlEscape(key)}</key>`,
      "\t<dict>",
      "\t\t<key>NSStringLocalizedFormatKey</key>",
      "\t\t<string>%#@count@</string>",
      "\t\t<key>count</key>",
      "\t\t<dict>",
      "\t\t\t<key>NSStringFormatSpecTypeKey</key>",
      "\t\t\t<string>NSStringPluralRuleType</string>",
      "\t\t\t<key>NSStringFormatValueTypeKey</key>",
      "\t\t\t<string>d</string>",
      `${cats}\t\t</dict>`,
      "\t</dict>",
      ""
    ].join("\n");
  }).join("");
  const header = '<?xml version="1.0" encoding="UTF-8"?>\n' +
    '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n';
  return `${header}<plist version="1.0">\n<dict>\n${entries}</dict>\n</plist>\n`;
}

function main() {
  const rootDir = process.cwd();
  const outDir = path.join(rootDir, "apps", "ios", "PangeaVPN", "Resources");
  const { tables, plurals } = buildAll(rootDir);
  for (const code of LOCALES) {
    const dir = path.join(outDir, `${LPROJ_DIR[code]}.lproj`);
    fs.mkdirSync(dir, { recursive: true });
    fs.writeFileSync(path.join(dir, "Localizable.strings"), render(tables[code]), "utf8");
    const dictPath = path.join(dir, "Localizable.stringsdict");
    if (Object.keys(plurals[code]).length > 0) fs.writeFileSync(dictPath, renderStringsdict(plurals[code]), "utf8");
    else fs.rmSync(dictPath, { force: true });
    console.log(`${LPROJ_DIR[code]}: ${Object.keys(tables[code]).length} keys, ${Object.keys(plurals[code]).length} plurals`);
  }
}

export { IOS_ONLY, LOCALES, buildAll, buildTables, parsePlural, renderStringsdict, toStringsValue };

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main();
}
