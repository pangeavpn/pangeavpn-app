// Persian/Farsi (fa) — machine translation, pending native review. RTL.
import type { Messages } from "../messages";

export const fa = {
  // ── App shell / loading ───────────────────────────────
  "app.brand": "Pangea VPN",
  "app.loading.starting": "در حال راه‌اندازی PangeaVPN...",
  "app.loading.progress": "در حال آماده‌سازی... ({remaining} ثانیه)",
  "app.loading.cantStart": "PangeaVPN نتوانست اجرا شود. لطفاً برنامه را دوباره اجرا کنید.",
  "app.loading.didntStart": "PangeaVPN اجرا نشد. لطفاً برنامه را دوباره اجرا کنید.",

  // ── Login screen ──────────────────────────────────────
  "login.subtitle": "برای ادامه، توکن ورود خود را وارد کنید.",
  "login.getToken": "دریافت توکن",
  "login.tokenPlaceholder": "0000000000000000",
  "login.tokenAriaLabel": "توکن ورود",
  "login.signIn": "ورود",
  "login.cachedTokenAriaLabel": "ورود با توکن ذخیره‌شده",
  "login.enterToken": "لطفاً توکن VPN خود را وارد کنید.",
  "login.signingIn": "در حال ورود...",
  "login.invalidToken": "توکن VPN نامعتبر است.",
  "login.signInFailed": "ورود ناموفق بود.",

  // ── Device-limit screen ───────────────────────────────
  "deviceLimit.title": "به سقف تعداد دستگاه‌ها رسیدید",
  "deviceLimit.subtitle": "حساب شما به حداکثر تعداد دستگاه‌ها رسیده است. برای ادامه ورود، یکی را حذف کنید.",
  "deviceLimit.continue": "ادامه ورود",
  "deviceLimit.cancel": "لغو و خروج",
  "deviceLimit.loadFailed": "بارگذاری دستگاه‌ها ممکن نشد. لطفاً دوباره تلاش کنید.",
  "deviceLimit.noneCanContinue": "دستگاهی یافت نشد. اکنون می‌توانید ورود را ادامه دهید.",
  "deviceLimit.stillAtLimit": "هنوز در سقف تعداد دستگاه‌ها هستید. لطفاً دستگاه دیگری را حذف کنید.",

  // ── Devices (modal + shared rows) ─────────────────────
  "devices.title": "دستگاه‌ها",
  "devices.subtitle": "دستگاه‌های واردشده به حساب شما.",
  "devices.thisDevice": "این دستگاه",
  "devices.added": "افزوده‌شده در {date}",
  "devices.current": "فعلی",
  "devices.currentTitle": "برای حذف این دستگاه خارج شوید",
  "devices.remove": "حذف",
  "devices.removing": "در حال حذف...",
  "devices.removed": "دستگاه حذف شد.",
  "devices.removeFailed": "حذف دستگاه ناموفق بود. لطفاً دوباره تلاش کنید.",
  "devices.none": "دستگاهی یافت نشد.",
  "devices.noneRemaining": "دستگاهی باقی نمانده است.",

  // ── Header / menu ─────────────────────────────────────
  "header.brandAriaLabel": "Pangea VPN",
  "header.signIn": "ورود",
  "header.toggleTheme": "تغییر پوسته",
  "header.menu": "منو",
  "menu.settings": "تنظیمات",
  "menu.updateAvailable": "به‌روزرسانی موجود است",

  // ── Hero connection card ──────────────────────────────
  "hero.selectServer": "انتخاب سرور",
  "hero.refreshServers": "به‌روزرسانی سرورها",
  "hero.connect": "اتصال",
  "hero.disconnect": "قطع اتصال",
  "hero.provisioning": "در حال آماده‌سازی...",
  "hero.disconnecting": "در حال قطع اتصال...",
  "hero.noServers": "سروری در دسترس نیست",

  // ── Status pills ──────────────────────────────────────
  "pill.killSwitch": "کلید قطع اضطراری",
  "pill.cloak": "استتار",
  "pill.wireguard": "WireGuard",

  // ── Connection state labels (hero + tray) ─────────────
  "state.DISCONNECTED": "قطع شده",
  "state.CONNECTING": "در حال اتصال",
  "state.CONNECTED": "متصل",
  "state.DISCONNECTING": "در حال قطع اتصال",
  "state.ERROR": "خطا",

  // ── Logs panel ────────────────────────────────────────
  "logs.title": "گزارش‌ها",
  "logs.copyDiagnostics": "کپی اطلاعات عیب‌یابی",
  "logs.copyLogs": "کپی گزارش‌ها",
  "logs.clear": "پاک کردن",
  "logs.noneToCopy": "گزارشی برای کپی وجود ندارد.",
  "logs.copied": "گزارش‌ها در کلیپ‌بورد کپی شد.",
  "logs.cleared": "گزارش‌ها پاک شد.",
  "logs.diagnosticsCopied": "اطلاعات عیب‌یابی در کلیپ‌بورد کپی شد.",
  "logs.bridgeUnavailable": "پل daemonApi در دسترس نیست.",

  // ── Settings overlay ──────────────────────────────────
  "settings.title": "تنظیمات",
  "settings.close": "بستن",
  "settings.account.heading": "حساب کاربری",
  "settings.account.signedInAs": "واردشده به عنوان",
  "settings.account.subscription": "اشتراک",
  "settings.account.token": "توکن ورود",
  "settings.account.show": "نمایش",
  "settings.account.hide": "پنهان",
  "settings.account.copy": "کپی",
  "settings.account.tokenHint": "این همان توکنی است که با آن وارد شده‌اید. آن را محرمانه نگه دارید — هرکسی آن را داشته باشد می‌تواند به حساب شما دسترسی پیدا کند.",
  "settings.account.manageSub": "مدیریت اشتراک",
  "settings.account.devices": "دستگاه‌ها",
  "settings.account.signOut": "خروج",
  "settings.censorship.heading": "دور زدن سانسور",
  "settings.censorship.description": "اگر شبکه شما دسترسی به سرویس‌های VPN را مسدود می‌کند، فعال کنید.",
  "settings.censorship.directIp.title": "IP مستقیم",
  "settings.censorship.directIp.hint": "اتصال به سرورهای ما از طریق آدرس IP، بدون استفاده از DNS.",
  "settings.censorship.directIpOnly.title": "فقط IP مستقیم",
  "settings.censorship.directIpOnly.hint": "همیشه از اتصال مستقیم IP استفاده کن. فراخوانی‌های عادی API را کاملاً نادیده می‌گیرد — اگر شبکه شما HTTPS به سرورهای ما را مسدود می‌کند، از آن استفاده کنید.",
  "settings.network.heading": "شبکه",
  "settings.network.description": "رفع مشکل برای شبکه‌های Wi-Fi محدودکننده.",
  "settings.network.allowLan.title": "اجازه به LAN",
  "settings.network.allowLan.hint": "به ترافیک شبکه محلی (روتر، چاپگرها، درگاه‌های ورود) اجازه بده تونل را دور بزند. اگر Wi-Fi شما هنگام اتصال گاه‌به‌گاه به حالت \"بدون اینترنت\" می‌رود، آن را روشن کنید. از اتصال بعدی اعمال می‌شود.",
  "settings.startup.heading": "راه‌اندازی",
  "settings.startup.description": "اجرای پس‌زمینه و اتصال مجدد خودکار.",
  "settings.startup.launch.title": "اجرا هنگام راه‌اندازی سیستم",
  "settings.startup.launch.hint": "PangeaVPN را هنگام ورود به سیستم به‌طور خودکار اجرا کن. پنهان باز می‌شود — برای دسترسی از آیکون سینی استفاده کنید.",
  "settings.startup.lockdown.title": "قفل کامل",
  "settings.startup.lockdown.hint": "PangeaVPN را هنگام راه‌اندازی سیستم اجرا کن (پنهان در سینی)، به‌طور خودکار به آخرین سرور متصل شو، و پس از قطع اتصال کلید قطع اضطراری را روشن نگه دار — فقط ترافیک VPN مجاز است. برای بازگرداندن اینترنت محافظت‌نشده، قفل کامل را خاموش کنید.",
  "settings.language.heading": "زبان",
  "settings.language.description": "زبان نمایش برنامه را انتخاب کنید.",
  "settings.language.system": "پیش‌فرض سیستم",
  "settings.language.restartHint": "برای اعمال زبان جدید، PangeaVPN را دوباره راه‌اندازی کنید.",
  "settings.update.heading": "به‌روزرسانی نرم‌افزار",
  "settings.update.currentVersion": "نسخه فعلی:",
  "settings.update.check": "بررسی به‌روزرسانی‌ها",

  // ── Server picker overlay ─────────────────────────────
  "serverPicker.title": "انتخاب سرور",
  "serverPicker.serversAriaLabel": "سرورها",
  "serverPicker.noServers": "سروری در دسترس نیست",
  "serverPicker.load": "بار سرور {pct}%",
  "serverPicker.loadPct": "{pct}%",

  // ── Update modal ──────────────────────────────────────
  "update.title": "به‌روزرسانی موجود است",
  "update.current": "فعلی",
  "update.latest": "جدیدترین",
  "update.macStep": "Terminal را باز کنید (⌘ + Space را فشار دهید، Terminal را تایپ کنید، Enter را بزنید)، سپس این دستور را جای‌گذاری کنید و Enter را بزنید:",
  "update.download": "دانلود به‌روزرسانی",
  "update.copyCommand": "کپی دستور نصب",
  "update.copied": "کپی شد!",
  "update.macPasteHint": "اکنون دستور را در Terminal جای‌گذاری کنید و Enter را بزنید.",
  "update.restartToUpdate": "برای به‌روزرسانی دوباره راه‌اندازی کنید",
  "update.readyToInstall": "به‌روزرسانی دانلود شد و آماده نصب است.",
  "update.opening": "در حال باز کردن...",
  "update.viewDownload": "مشاهده دانلود",
  "update.retry": "تلاش مجدد",
  "update.retryDownload": "تلاش مجدد برای دانلود",
  "update.checking": "در حال بررسی...",
  "update.onLatest": "شما از جدیدترین نسخه استفاده می‌کنید.",
  "update.checkFailed": "بررسی به‌روزرسانی‌ها ممکن نشد. بعداً دوباره تلاش کنید.",
  "update.unavailable": "به‌روزرسانی‌ها در این نسخه در دسترس نیستند.",

  // ── Account / subscription ────────────────────────────
  "sub.none": "اشتراک فعالی وجود ندارد",
  "sub.trialPrefix": "دوره آزمایشی رایگان · ",
  "sub.renews": "تمدید می‌شود",
  "sub.expires": "منقضی می‌شود",
  "sub.pastDue": "پرداخت معوق",
  "account.noToken": "توکنی برای کپی وجود ندارد.",
  "account.tokenCopied": "توکن در کلیپ‌بورد کپی شد.",

  // ── Session / auth toasts ─────────────────────────────
  "auth.signedOutRetry": "شما از حساب خارج شده‌اید. لطفاً دوباره وارد شوید.",
  "auth.signingOut": "در حال خروج...",
  "auth.signedOut": "خارج شدید.",
  "auth.deviceNamed": "نام دستگاه شما \"{name}\" است.",

  // ── Connect / disconnect flow ─────────────────────────
  "connect.noServer": "سروری انتخاب نشده است.",
  "connect.provisioning": "در حال آماده‌سازی و اتصال...",
  "connect.connected": "متصل شد.",
  "connect.failed": "اتصال ممکن نشد. دوباره تلاش کنید یا سرور دیگری انتخاب کنید.",
  "connect.switching": "در حال آماده‌سازی سرور جدید...",
  "connect.switchFailed": "تعویض سرور ممکن نشد. دوباره تلاش کنید یا سرور دیگری انتخاب کنید.",
  "connect.disconnecting": "در حال قطع اتصال...",
  "connect.disconnected": "اتصال قطع شد.",
  "connect.disconnectFailed": "قطع اتصال ممکن نشد. لطفاً دوباره تلاش کنید.",
  "connect.recovered": "اتصال بازیابی شد.",
  "connect.refreshServersFailed": "به‌روزرسانی فهرست سرورها ممکن نشد. به‌طور خودکار دوباره تلاش می‌شود.",

  // ── Settings toggles ──────────────────────────────────
  "toggle.updateFailed": "به‌روزرسانی تنظیمات ناموفق بود.",
  "toggle.directIp.on": "IP مستقیم فعال شد.",
  "toggle.directIp.off": "IP مستقیم غیرفعال شد.",
  "toggle.directIpOnly.on": "حالت فقط IP مستقیم فعال شد.",
  "toggle.directIpOnly.off": "حالت فقط IP مستقیم غیرفعال شد.",
  "toggle.allowLan.on": "اجازه به LAN فعال شد. برای اعمال، دوباره متصل شوید.",
  "toggle.allowLan.off": "اجازه به LAN غیرفعال شد. برای اعمال، دوباره متصل شوید.",
  "toggle.launch.on": "PangeaVPN هنگام راه‌اندازی سیستم اجرا خواهد شد.",
  "toggle.launch.off": "اجرا هنگام راه‌اندازی سیستم غیرفعال شد.",
  "toggle.launch.failed": "به‌روزرسانی تنظیمات راه‌اندازی ناموفق بود.",
  "toggle.launch.packagedOnly": "فقط در نسخه‌های بسته‌بندی‌شده در دسترس است",
  "toggle.lockdown.failed": "به‌روزرسانی قفل کامل ناموفق بود.",
  "toggle.lockdown.on": "قفل کامل روشن شد — اتصال خودکار فعال است و کلید قطع اضطراری تا زمانی که آن را خاموش کنید روشن می‌ماند.",
  "toggle.lockdown.off": "قفل کامل خاموش شد — اینترنت عادی بازگردانده شد.",

  // ── Verbose errors (hidden debug toggle) ──────────────
  "verbose.on": "خطاهای مفصل فعال شد",
  "verbose.off": "خطاهای مفصل غیرفعال شد",

  // ── Daemon sync / generic ─────────────────────────────
  "common.loading": "در حال بارگذاری...",
  "common.ready": "آماده.",
  "common.retrying": "مشکلی پیش آمد. در حال تلاش مجدد...",
  "common.dash": "—",

  // ── Daemon status values (technical, kept short) ──────
  "status.running": "در حال اجرا",
  "status.stopped": "متوقف",

  // ── Generic error fallbacks ───────────────────────────
  "error.network": "لطفاً اتصال اینترنت خود را بررسی کنید و دوباره تلاش کنید.",
  "error.generic": "مشکلی پیش آمد. لطفاً بعداً دوباره تلاش کنید."
} satisfies Messages;
