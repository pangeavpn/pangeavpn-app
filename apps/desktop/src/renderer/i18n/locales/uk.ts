// Ukrainian (uk) — machine translation, pending native review.
import type { Messages } from "../messages";

export const uk = {
  // ── App shell / loading ───────────────────────────────
  "app.brand": "Pangea VPN",
  "app.loading.starting": "Запуск PangeaVPN...",
  "app.loading.progress": "Готуємо все... ({remaining} с)",
  "app.loading.cantStart": "Не вдалося запустити PangeaVPN. Перезапустіть застосунок.",
  "app.loading.didntStart": "PangeaVPN не запустився. Перезапустіть застосунок.",

  // ── Login screen ──────────────────────────────────────
  "login.subtitle": "Введіть токен для входу, щоб продовжити.",
  "login.getToken": "Отримати токен",
  "login.tokenPlaceholder": "0000000000000000",
  "login.tokenAriaLabel": "Токен для входу",
  "login.signIn": "Увійти",
  "login.cachedTokenAriaLabel": "Увійти зі збереженим токеном",
  "login.enterToken": "Введіть свій токен VPN.",
  "login.signingIn": "Вхід...",
  "login.invalidToken": "Недійсний токен VPN.",
  "login.signInFailed": "Не вдалося увійти.",

  // ── Device-limit screen ───────────────────────────────
  "deviceLimit.title": "Досягнуто ліміту пристроїв",
  "deviceLimit.subtitle": "Ваш обліковий запис досяг максимальної кількості пристроїв. Видаліть один, щоб продовжити вхід.",
  "deviceLimit.continue": "Продовжити вхід",
  "deviceLimit.cancel": "Скасувати та вийти",
  "deviceLimit.loadFailed": "Не вдалося завантажити пристрої. Спробуйте ще раз.",
  "deviceLimit.noneCanContinue": "Пристроїв не знайдено. Тепер можете продовжити вхід.",
  "deviceLimit.stillAtLimit": "Ліміт пристроїв досі перевищено. Видаліть ще один пристрій.",

  // ── Devices (modal + shared rows) ─────────────────────
  "devices.title": "Пристрої",
  "devices.subtitle": "Пристрої, що ввійшли у ваш обліковий запис.",
  "devices.thisDevice": "Цей пристрій",
  "devices.added": "Додано {date}",
  "devices.current": "Поточний",
  "devices.currentTitle": "Вийдіть, щоб видалити цей пристрій",
  "devices.remove": "Видалити",
  "devices.removing": "Видалення...",
  "devices.removed": "Пристрій видалено.",
  "devices.removeFailed": "Не вдалося видалити пристрій. Спробуйте ще раз.",
  "devices.none": "Пристроїв не знайдено.",
  "devices.noneRemaining": "Пристроїв не залишилося.",

  // ── Header / menu ─────────────────────────────────────
  "header.brandAriaLabel": "Pangea VPN",
  "header.signIn": "Увійти",
  "header.toggleTheme": "Змінити тему",
  "header.menu": "Меню",
  "menu.settings": "Налаштування",
  "menu.updateAvailable": "Доступне оновлення",

  // ── Hero connection card ──────────────────────────────
  "hero.selectServer": "Вибрати сервер",
  "hero.refreshServers": "Оновити сервери",
  "hero.connect": "Підключитися",
  "hero.disconnect": "Відключитися",
  "hero.provisioning": "Підготовка...",
  "hero.disconnecting": "Відключення...",
  "hero.noServers": "Немає доступних серверів",

  // ── Status pills ──────────────────────────────────────
  "pill.killSwitch": "Аварійне вимкнення",
  "pill.cloak": "Маскування",
  "pill.wireguard": "WireGuard",

  // ── Connection state labels (hero + tray) ─────────────
  "state.DISCONNECTED": "ВІДКЛЮЧЕНО",
  "state.CONNECTING": "ПІДКЛЮЧЕННЯ",
  "state.CONNECTED": "ПІДКЛЮЧЕНО",
  "state.DISCONNECTING": "ВІДКЛЮЧЕННЯ",
  "state.ERROR": "ПОМИЛКА",

  // ── Logs panel ────────────────────────────────────────
  "logs.title": "Журнали",
  "logs.copyDiagnostics": "Копіювати діагностику",
  "logs.copyLogs": "Копіювати журнали",
  "logs.clear": "Очистити",
  "logs.noneToCopy": "Немає журналів для копіювання.",
  "logs.copied": "Журнали скопійовано до буфера обміну.",
  "logs.cleared": "Журнали очищено.",
  "logs.diagnosticsCopied": "Діагностику скопійовано до буфера обміну.",
  "logs.bridgeUnavailable": "Міст daemonApi недоступний.",

  // ── Settings overlay ──────────────────────────────────
  "settings.title": "Налаштування",
  "settings.close": "Закрити",
  "settings.account.heading": "Обліковий запис",
  "settings.account.signedInAs": "Ви ввійшли як",
  "settings.account.subscription": "Підписка",
  "settings.account.token": "Токен для входу",
  "settings.account.show": "Показати",
  "settings.account.hide": "Сховати",
  "settings.account.copy": "Копіювати",
  "settings.account.tokenHint": "Це токен, з яким ви ввійшли. Тримайте його в таємниці — будь-хто з ним може отримати доступ до вашого облікового запису.",
  "settings.account.manageSub": "Керувати підпискою",
  "settings.account.devices": "Пристрої",
  "settings.account.signOut": "Вийти",
  "settings.censorship.heading": "Обхід цензури",
  "settings.censorship.description": "Увімкніть, якщо ваша мережа блокує доступ до сервісів VPN.",
  "settings.censorship.directIp.title": "Пряме IP",
  "settings.censorship.directIp.hint": "Підключайтеся до наших серверів за IP-адресою, повністю оминаючи DNS.",
  "settings.censorship.directIpOnly.title": "Лише пряме IP",
  "settings.censorship.directIpOnly.hint": "Завжди використовувати прямі IP-з'єднання. Повністю оминає звичайні виклики API — використовуйте, якщо ваша мережа блокує HTTPS до наших серверів.",
  "settings.transport.heading": "Connection Method",
  "settings.transport.description": "How PangeaVPN disguises your traffic.",
  "settings.transport.auto": "Automatic (recommended)",
  "settings.transport.cloak": "Cloak only",
  "settings.transport.naive": "NaiveProxy only",
  "settings.transport.reality": "VLESS+REALITY only",
  "settings.transport.hysteria2": "Hysteria2 only",
  "settings.transport.snowflake": "Snowflake only",
  "settings.network.heading": "Мережа",
  "settings.network.description": "Виправлення для обмежувальних мереж Wi-Fi.",
  "settings.network.allowLan.title": "Дозволити LAN",
  "settings.network.allowLan.hint": "Дозволити трафіку локальної мережі (маршрутизатор, принтери, кептив-портали) оминати тунель. Увімкніть, якщо ваш Wi-Fi періодично показує \"Немає інтернету\" під час підключення. Набуває чинності під час наступного підключення.",
  "settings.startup.heading": "Автозапуск",
  "settings.startup.description": "Фоновий запуск та автоматичне перепідключення.",
  "settings.startup.launch.title": "Запускати під час старту системи",
  "settings.startup.launch.hint": "Автоматично запускати PangeaVPN під час входу в систему. Відкривається прихованим — використовуйте піктограму в треї для доступу.",
  "settings.startup.lockdown.title": "Блокування",
  "settings.startup.lockdown.hint": "Запускати PangeaVPN під час старту (приховано в треї), автоматично підключатися до останнього сервера та тримати аварійне вимкнення увімкненим після відключення — дозволено лише трафік VPN. Вимкніть Блокування, щоб повернути незахищений інтернет.",
  "settings.language.heading": "Мова",
  "settings.language.description": "Виберіть мову інтерфейсу застосунку.",
  "settings.language.system": "Як у системі",
  "settings.language.restartHint": "Перезапустіть PangeaVPN, щоб застосувати нову мову.",
  "settings.update.heading": "Оновлення програми",
  "settings.update.currentVersion": "Поточна версія:",
  "settings.update.check": "Перевірити оновлення",

  // ── Server picker overlay ─────────────────────────────
  "serverPicker.title": "Вибрати сервер",
  "serverPicker.serversAriaLabel": "Сервери",
  "serverPicker.noServers": "Немає доступних серверів",
  "serverPicker.load": "Завантаження сервера {pct}%",
  "serverPicker.loadPct": "{pct}%",

  // ── Update modal ──────────────────────────────────────
  "update.title": "Доступне оновлення",
  "update.current": "Поточна",
  "update.latest": "Остання",
  "update.macStep": "Відкрийте Terminal (натисніть ⌘ + Space, введіть Terminal, натисніть Enter), потім вставте цю команду й натисніть Enter:",
  "update.download": "Завантажити оновлення",
  "update.copyCommand": "Копіювати команду встановлення",
  "update.copied": "Скопійовано!",
  "update.macPasteHint": "Тепер вставте команду в Terminal і натисніть Enter.",
  "update.restartToUpdate": "Перезапустити для оновлення",
  "update.readyToInstall": "Оновлення завантажено та готове до встановлення.",
  "update.opening": "Відкриття...",
  "update.viewDownload": "Переглянути завантаження",
  "update.retry": "Повторити",
  "update.retryDownload": "Повторити завантаження",
  "update.checking": "Перевірка...",
  "update.onLatest": "У вас найновіша версія.",
  "update.checkFailed": "Не вдалося перевірити оновлення. Спробуйте пізніше.",
  "update.unavailable": "Оновлення недоступні в цій збірці.",

  // ── Account / subscription ────────────────────────────
  "sub.none": "Немає активної підписки",
  "sub.trialPrefix": "Безкоштовний період · ",
  "sub.renews": "Поновлюється",
  "sub.expires": "Закінчується",
  "sub.pastDue": "Прострочений платіж",
  "account.noToken": "Немає токена для копіювання.",
  "account.tokenCopied": "Токен скопійовано до буфера обміну.",

  // ── Session / auth toasts ─────────────────────────────
  "auth.signedOutRetry": "Ви вийшли із системи. Будь ласка, увійдіть знову.",
  "auth.signingOut": "Вихід...",
  "auth.signedOut": "Ви вийшли.",
  "auth.deviceNamed": "Ваш пристрій має назву \"{name}\".",

  // ── Connect / disconnect flow ─────────────────────────
  "connect.noServer": "Сервер не вибрано.",
  "connect.provisioning": "Підготовка та підключення...",
  "connect.connected": "Підключено.",
  "connect.failed": "Не вдалося підключитися. Спробуйте ще раз або виберіть інший сервер.",
  "connect.switching": "Підготовка нового сервера...",
  "connect.switchFailed": "Не вдалося змінити сервер. Спробуйте ще раз або виберіть інший сервер.",
  "connect.disconnecting": "Відключення...",
  "connect.disconnected": "Відключено.",
  "connect.disconnectFailed": "Не вдалося відключитися. Спробуйте ще раз.",
  "connect.recovered": "З'єднання відновлено.",
  "connect.refreshServersFailed": "Не вдалося оновити список серверів. Спробу буде повторено автоматично.",

  // ── Settings toggles ──────────────────────────────────
  "toggle.updateFailed": "Не вдалося оновити налаштування.",
  "toggle.directIp.on": "Пряме IP увімкнено.",
  "toggle.directIp.off": "Пряме IP вимкнено.",
  "toggle.directIpOnly.on": "Режим лише прямого IP увімкнено.",
  "toggle.directIpOnly.off": "Режим лише прямого IP вимкнено.",
  "toggle.allowLan.on": "Дозвіл LAN увімкнено. Перепідключіться, щоб зміни набули чинності.",
  "toggle.allowLan.off": "Дозвіл LAN вимкнено. Перепідключіться, щоб зміни набули чинності.",
  "toggle.preferredTransport.updated": "Connection method updated. Reconnect for it to take effect.",
  "toggle.launch.on": "PangeaVPN запускатиметься під час старту системи.",
  "toggle.launch.off": "Запуск під час старту системи вимкнено.",
  "toggle.launch.failed": "Не вдалося оновити налаштування автозапуску.",
  "toggle.launch.packagedOnly": "Доступно лише в упакованих збірках",
  "toggle.lockdown.failed": "Не вдалося оновити Блокування.",
  "toggle.lockdown.on": "Блокування увімкнено — автопідключення активне, а аварійне вимкнення залишається увімкненим, доки ви його не вимкнете.",
  "toggle.lockdown.off": "Блокування вимкнено — звичайний інтернет відновлено.",

  // ── Verbose errors (hidden debug toggle) ──────────────
  "verbose.on": "Детальні помилки увімкнено",
  "verbose.off": "Детальні помилки вимкнено",

  // ── Daemon sync / generic ─────────────────────────────
  "common.loading": "Завантаження...",
  "common.ready": "Готово.",
  "common.retrying": "Щось пішло не так. Повторна спроба...",
  "common.dash": "—",

  // ── Daemon status values (technical, kept short) ──────
  "status.running": "працює",
  "status.stopped": "зупинено",
  "status.transport.cloak": "Obfuscation: Cloak",
  "status.transport.naive": "Obfuscation: NaiveProxy",
  "status.transport.hysteria2": "Obfuscation: Hysteria2",
  "status.transport.snowflake": "Obfuscation: Snowflake",
  "status.transport.none": "",

  // ── Generic error fallbacks ───────────────────────────
  "error.network": "Перевірте підключення до інтернету та спробуйте ще раз.",
  "error.generic": "Щось пішло не так. Спробуйте пізніше."
} satisfies Messages;
