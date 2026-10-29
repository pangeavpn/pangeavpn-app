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
    en: "Kill switch", es: "Interruptor de seguridad", fr: "Kill switch", ru: "Аварийное отключение",
    uk: "Аварійне відключення", zh: "断网保护", ar: "مفتاح الإيقاف", fa: "قطع‌کن اضطراری"
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

function toStringsValue(xmlValue) {
  const plain = xmlValue
    .replace(/&(amp|lt|gt|quot|apos);/g, (_, name) => XML_ENTITIES[name])
    .replace(/\\(.)/gs, (_, ch) => ANDROID_ESCAPES[ch] ?? ch)
    .replace(/%(\d+)\$s/g, "%$1$$@");
  return plain
    .replace(/\\/g, "\\\\")
    .replace(/"/g, '\\"')
    .replace(/\n/g, "\\n")
    .replace(/\t/g, "\\t");
}

function parseAndroid(xml, file) {
  const entries = {};
  for (const m of xml.matchAll(/<string name="([^"]+)"[^>]*>(.*?)<\/string>/gs)) {
    if (m[1] in entries) throw new Error(`Duplicate key "${m[1]}" in ${file}`);
    entries[m[1]] = m[2];
  }
  return entries;
}

function buildTables(rootDir) {
  const tables = {};
  for (const code of LOCALES) {
    const merged = {};
    for (const mod of ANDROID_MODULES) {
      const file = path.join(rootDir, "apps", "android", mod, "src", "main", "res", ANDROID_DIR[code], "strings.xml");
      for (const [key, value] of Object.entries(parseAndroid(fs.readFileSync(file, "utf8"), file))) {
        if (key in merged) throw new Error(`Key "${key}" appears in more than one module for ${code}`);
        merged[key] = toStringsValue(value);
      }
    }
    for (const [key, translations] of Object.entries(IOS_ONLY)) {
      if (key in merged) throw new Error(`IOS_ONLY key "${key}" collides with an Android key`);
      if (translations[code] === undefined) throw new Error(`Missing iOS-only translation "${key}" for "${code}"`);
      merged[key] = toStringsValue(translations[code]);
    }
    tables[code] = Object.fromEntries(Object.entries(merged).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)));
  }
  return tables;
}

function render(table) {
  return Object.entries(table).map(([k, v]) => `"${k}" = "${v}";\n`).join("");
}

function main() {
  const rootDir = process.cwd();
  const outDir = path.join(rootDir, "apps", "ios", "PangeaVPN", "Resources");
  const tables = buildTables(rootDir);
  for (const code of LOCALES) {
    const dir = path.join(outDir, `${LPROJ_DIR[code]}.lproj`);
    fs.mkdirSync(dir, { recursive: true });
    fs.writeFileSync(path.join(dir, "Localizable.strings"), render(tables[code]), "utf8");
    console.log(`${LPROJ_DIR[code]}: ${Object.keys(tables[code]).length} keys`);
  }
}

export { IOS_ONLY, LOCALES, buildTables, toStringsValue };

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main();
}
