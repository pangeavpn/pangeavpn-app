// Arabic (ar) — machine translation, pending native review. RTL.
import type { Messages } from "../messages";

export const ar = {
  // ── App shell / loading ───────────────────────────────
  "app.brand": "Pangea VPN",
  "app.loading.starting": "جارٍ تشغيل PangeaVPN...",
  "app.loading.progress": "جارٍ التجهيز... ({remaining}ث)",
  "app.loading.cantStart": "تعذّر تشغيل PangeaVPN. يرجى إعادة تشغيل التطبيق.",
  "app.loading.didntStart": "لم يبدأ PangeaVPN. يرجى إعادة تشغيل التطبيق.",

  // ── Login screen ──────────────────────────────────────
  "login.subtitle": "أدخل رمز الدخول للمتابعة.",
  "login.getToken": "احصل على رمزك",
  "login.tokenPlaceholder": "0000000000000000",
  "login.tokenAriaLabel": "رمز الدخول",
  "login.signIn": "تسجيل الدخول",
  "login.cachedTokenAriaLabel": "تسجيل الدخول بالرمز المحفوظ",
  "login.enterToken": "يرجى إدخال رمز VPN الخاص بك.",
  "login.signingIn": "جارٍ تسجيل الدخول...",
  "login.invalidToken": "رمز VPN غير صالح.",
  "login.signInFailed": "فشل تسجيل الدخول.",

  // ── Device-limit screen ───────────────────────────────
  "deviceLimit.title": "تم بلوغ حد الأجهزة",
  "deviceLimit.subtitle": "وصل حسابك إلى الحد الأقصى لعدد الأجهزة. أزل جهازًا لمتابعة تسجيل الدخول.",
  "deviceLimit.continue": "متابعة تسجيل الدخول",
  "deviceLimit.cancel": "إلغاء وتسجيل الخروج",
  "deviceLimit.loadFailed": "تعذّر تحميل الأجهزة. يرجى المحاولة مرة أخرى.",
  "deviceLimit.noneCanContinue": "لم يتم العثور على أجهزة. يمكنك متابعة تسجيل الدخول الآن.",
  "deviceLimit.stillAtLimit": "ما زلت عند حد الأجهزة. يرجى إزالة جهاز آخر.",

  // ── Devices (modal + shared rows) ─────────────────────
  "devices.title": "الأجهزة",
  "devices.subtitle": "الأجهزة المسجّلة الدخول إلى حسابك.",
  "devices.thisDevice": "هذا الجهاز",
  "devices.added": "أُضيف في {date}",
  "devices.current": "الحالي",
  "devices.currentTitle": "سجّل الخروج لإزالة هذا الجهاز",
  "devices.remove": "إزالة",
  "devices.removing": "جارٍ الإزالة...",
  "devices.removed": "تمت إزالة الجهاز.",
  "devices.removeFailed": "فشل إزالة الجهاز. يرجى المحاولة مرة أخرى.",
  "devices.none": "لم يتم العثور على أجهزة.",
  "devices.noneRemaining": "لم يتبقَّ أي جهاز.",

  // ── Header / menu ─────────────────────────────────────
  "header.brandAriaLabel": "Pangea VPN",
  "header.signIn": "تسجيل الدخول",
  "header.toggleTheme": "تبديل المظهر",
  "header.menu": "القائمة",
  "menu.settings": "الإعدادات",
  "menu.updateAvailable": "يتوفّر تحديث",

  // ── Hero connection card ──────────────────────────────
  "hero.selectServer": "اختر خادمًا",
  "hero.refreshServers": "تحديث الخوادم",
  "hero.connect": "اتصال",
  "hero.disconnect": "قطع الاتصال",
  "hero.provisioning": "جارٍ التجهيز...",
  "hero.disconnecting": "جارٍ قطع الاتصال...",
  "hero.noServers": "لا توجد خوادم متاحة",

  // ── Status pills ──────────────────────────────────────
  "pill.killSwitch": "مفتاح الإيقاف",
  "pill.cloak": "التخفّي",
  "pill.wireguard": "WireGuard",

  // ── Connection state labels (hero + tray) ─────────────
  "state.DISCONNECTED": "غير متصل",
  "state.CONNECTING": "جارٍ الاتصال",
  "state.CONNECTED": "متصل",
  "state.DISCONNECTING": "جارٍ قطع الاتصال",
  "state.ERROR": "خطأ",

  // ── Logs panel ────────────────────────────────────────
  "logs.title": "السجلّات",
  "logs.copyDiagnostics": "نسخ التشخيصات",
  "logs.copyLogs": "نسخ السجلّات",
  "logs.clear": "مسح",
  "logs.noneToCopy": "لا توجد سجلّات لنسخها.",
  "logs.copied": "تم نسخ السجلّات إلى الحافظة.",
  "logs.cleared": "تم مسح السجلّات.",
  "logs.diagnosticsCopied": "تم نسخ التشخيصات إلى الحافظة.",
  "logs.bridgeUnavailable": "جسر daemonApi غير متاح.",

  // ── Settings overlay ──────────────────────────────────
  "settings.title": "الإعدادات",
  "settings.close": "إغلاق",
  "settings.account.heading": "الحساب",
  "settings.account.signedInAs": "مسجّل الدخول باسم",
  "settings.account.subscription": "الاشتراك",
  "settings.account.token": "رمز الدخول",
  "settings.account.show": "إظهار",
  "settings.account.hide": "إخفاء",
  "settings.account.copy": "نسخ",
  "settings.account.tokenHint": "هذا هو الرمز الذي سجّلت الدخول به. احتفظ به سرًّا — أي شخص يملكه يمكنه الوصول إلى حسابك.",
  "settings.account.manageSub": "إدارة الاشتراك",
  "settings.account.devices": "الأجهزة",
  "settings.account.signOut": "تسجيل الخروج",
  "settings.censorship.heading": "تجاوز الحجب",
  "settings.censorship.description": "فعّله إذا كانت شبكتك تحجب الوصول إلى خدمات VPN.",
  "settings.censorship.directIp.title": "IP مباشر",
  "settings.censorship.directIp.hint": "اتصل بخوادمنا عبر عنوان IP، متجاوزًا DNS بالكامل.",
  "settings.censorship.directIpOnly.title": "IP مباشر فقط",
  "settings.censorship.directIpOnly.hint": "استخدم دائمًا اتصالات IP المباشرة. يتجاوز طلبات API العادية بالكامل — استخدمه إذا كانت شبكتك تحجب HTTPS عن خوادمنا.",
  "settings.transport.heading": "Connection Method",
  "settings.transport.description": "How PangeaVPN disguises your traffic.",
  "settings.transport.auto": "Automatic (recommended)",
  "settings.transport.cloak": "Cloak only",
  "settings.transport.naive": "NaiveProxy only",
  "settings.transport.reality": "VLESS+REALITY only",
  "settings.transport.hysteria2": "Hysteria2 only",
  "settings.network.heading": "الشبكة",
  "settings.network.description": "إصلاحات لشبكات Wi-Fi المقيّدة.",
  "settings.network.allowLan.title": "السماح بشبكة LAN",
  "settings.network.allowLan.hint": "اسمح لحركة الشبكة المحلية (الراوتر، الطابعات، البوابات المقيّدة) بتجاوز النفق. فعّله إذا كانت شبكة Wi-Fi تنقطع أحيانًا إلى \"لا يوجد إنترنت\" أثناء الاتصال. يسري عند الاتصال التالي.",
  "settings.startup.heading": "بدء التشغيل",
  "settings.startup.description": "التشغيل في الخلفية وإعادة الاتصال التلقائي.",
  "settings.startup.launch.title": "التشغيل عند بدء التشغيل",
  "settings.startup.launch.hint": "ابدأ PangeaVPN تلقائيًا عند تسجيل الدخول. يفتح مخفيًا — استخدم أيقونة شريط النظام للوصول إليه.",
  "settings.startup.lockdown.title": "الإغلاق التام",
  "settings.startup.lockdown.hint": "شغّل PangeaVPN عند بدء التشغيل (مخفيًا في شريط النظام)، واتصل تلقائيًا بآخر خادم، وأبقِ مفتاح الإيقاف مفعّلًا بعد قطع الاتصال — يُسمح بحركة VPN فقط. أوقف الإغلاق التام لاستعادة الإنترنت غير المحمي.",
  "settings.language.heading": "اللغة",
  "settings.language.description": "اختر لغة عرض التطبيق.",
  "settings.language.system": "الافتراضي للنظام",
  "settings.language.restartHint": "أعد تشغيل PangeaVPN لتطبيق اللغة الجديدة.",
  "settings.update.heading": "تحديث البرنامج",
  "settings.update.currentVersion": "الإصدار الحالي:",
  "settings.update.check": "التحقق من التحديثات",

  // ── Server picker overlay ─────────────────────────────
  "serverPicker.title": "اختر خادمًا",
  "serverPicker.serversAriaLabel": "الخوادم",
  "serverPicker.noServers": "لا توجد خوادم متاحة",
  "serverPicker.load": "حمل الخادم {pct}%",
  "serverPicker.loadPct": "{pct}%",

  // ── Update modal ──────────────────────────────────────
  "update.title": "يتوفّر تحديث",
  "update.current": "الحالي",
  "update.latest": "الأحدث",
  "update.macStep": "افتح Terminal (اضغط ⌘ + المسافة، اكتب Terminal، ثم اضغط Enter)، ثم الصق هذا الأمر واضغط Enter:",
  "update.download": "تنزيل التحديث",
  "update.copyCommand": "نسخ أمر التثبيت",
  "update.copied": "تم النسخ!",
  "update.macPasteHint": "الآن الصق الأمر في Terminal واضغط Enter.",
  "update.restartToUpdate": "أعد التشغيل للتحديث",
  "update.readyToInstall": "تم تنزيل التحديث وهو جاهز للتثبيت.",
  "update.opening": "جارٍ الفتح...",
  "update.viewDownload": "عرض التنزيل",
  "update.retry": "إعادة المحاولة",
  "update.retryDownload": "إعادة التنزيل",
  "update.checking": "جارٍ التحقق...",
  "update.onLatest": "أنت على أحدث إصدار.",
  "update.checkFailed": "تعذّر التحقق من التحديثات. حاول لاحقًا.",
  "update.unavailable": "التحديثات غير متاحة في هذه النسخة.",

  // ── Account / subscription ────────────────────────────
  "sub.none": "لا يوجد اشتراك نشط",
  "sub.trialPrefix": "تجربة مجانية · ",
  "sub.renews": "يتجدّد",
  "sub.expires": "ينتهي",
  "sub.pastDue": "الدفع متأخر",
  "account.noToken": "لا يوجد رمز لنسخه.",
  "account.tokenCopied": "تم نسخ الرمز إلى الحافظة.",

  // ── Session / auth toasts ─────────────────────────────
  "auth.signedOutRetry": "تم تسجيل خروجك. يرجى تسجيل الدخول مرة أخرى.",
  "auth.signingOut": "جارٍ تسجيل الخروج...",
  "auth.signedOut": "تم تسجيل الخروج.",
  "auth.deviceNamed": "اسم جهازك هو \"{name}\".",

  // ── Connect / disconnect flow ─────────────────────────
  "connect.noServer": "لم يتم اختيار خادم.",
  "connect.provisioning": "جارٍ التجهيز والاتصال...",
  "connect.connected": "تم الاتصال.",
  "connect.failed": "تعذّر الاتصال. حاول مرة أخرى، أو اختر خادمًا آخر.",
  "connect.switching": "جارٍ تجهيز خادم جديد...",
  "connect.switchFailed": "تعذّر تبديل الخوادم. حاول مرة أخرى، أو اختر خادمًا آخر.",
  "connect.disconnecting": "جارٍ قطع الاتصال...",
  "connect.disconnected": "تم قطع الاتصال.",
  "connect.disconnectFailed": "تعذّر قطع الاتصال. يرجى المحاولة مرة أخرى.",
  "connect.recovered": "تمت استعادة الاتصال.",
  "connect.refreshServersFailed": "تعذّر تحديث قائمة الخوادم. ستتم إعادة المحاولة تلقائيًا.",

  // ── Settings toggles ──────────────────────────────────
  "toggle.updateFailed": "فشل تحديث الإعداد.",
  "toggle.directIp.on": "تم تفعيل IP المباشر.",
  "toggle.directIp.off": "تم تعطيل IP المباشر.",
  "toggle.directIpOnly.on": "تم تفعيل وضع IP المباشر فقط.",
  "toggle.directIpOnly.off": "تم تعطيل وضع IP المباشر فقط.",
  "toggle.allowLan.on": "تم تفعيل السماح بشبكة LAN. أعد الاتصال ليسري المفعول.",
  "toggle.allowLan.off": "تم تعطيل السماح بشبكة LAN. أعد الاتصال ليسري المفعول.",
  "toggle.preferredTransport.updated": "Connection method updated. Reconnect for it to take effect.",
  "toggle.launch.on": "سيبدأ PangeaVPN عند بدء التشغيل.",
  "toggle.launch.off": "تم تعطيل التشغيل عند بدء التشغيل.",
  "toggle.launch.failed": "فشل تحديث إعداد بدء التشغيل.",
  "toggle.launch.packagedOnly": "متاح في النسخ المجمّعة فقط",
  "toggle.lockdown.failed": "فشل تحديث الإغلاق التام.",
  "toggle.lockdown.on": "الإغلاق التام مفعّل — تم تفعيل الاتصال التلقائي ويبقى مفتاح الإيقاف مفعّلًا حتى توقفه.",
  "toggle.lockdown.off": "الإغلاق التام متوقف — تمت استعادة الإنترنت العادي.",

  // ── Verbose errors (hidden debug toggle) ──────────────
  "verbose.on": "تم تفعيل الأخطاء المفصّلة",
  "verbose.off": "تم تعطيل الأخطاء المفصّلة",

  // ── Daemon sync / generic ─────────────────────────────
  "common.loading": "جارٍ التحميل...",
  "common.ready": "جاهز.",
  "common.retrying": "حدث خطأ ما. جارٍ إعادة المحاولة...",
  "common.dash": "—",

  // ── Daemon status values (technical, kept short) ──────
  "status.running": "قيد التشغيل",
  "status.stopped": "متوقف",
  "status.transport.cloak": "Obfuscation: Cloak",
  "status.transport.naive": "Obfuscation: NaiveProxy",
  "status.transport.hysteria2": "Obfuscation: Hysteria2",
  "status.transport.none": "",

  // ── Generic error fallbacks ───────────────────────────
  "error.network": "يرجى التحقق من اتصالك بالإنترنت والمحاولة مرة أخرى.",
  "error.generic": "حدث خطأ ما. يرجى المحاولة لاحقًا."
} satisfies Messages;
