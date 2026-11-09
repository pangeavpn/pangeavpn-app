// Kill Switch, Startup, Notifications and Language pages: Android wording where the iOS text names iOS.
export default {
  kill_switch_android_restart: {
    en: "With Auto-connect on, PangeaVPN reconnects after drops and after your device restarts.",
    es: "Con la conexión automática activada, PangeaVPN se reconecta tras una caída y después de reiniciar el dispositivo.",
    fr: "Avec la connexion automatique activée, PangeaVPN se reconnecte après une coupure et après le redémarrage de votre appareil.",
    ru: "Когда включено автоподключение, PangeaVPN переподключается после обрывов и после перезагрузки устройства.",
    uk: "Коли увімкнено автопідключення, PangeaVPN перепідключається після обривів і після перезавантаження пристрою.",
    zh: "开启自动连接时，PangeaVPN 会在断线后和设备重新启动后重新连接。",
    ar: "عند تفعيل الاتصال التلقائي، يعيد PangeaVPN الاتصال بعد الانقطاع وبعد إعادة تشغيل جهازك.",
    fa: "وقتی اتصال خودکار روشن است، PangeaVPN پس از قطعی و پس از راه‌اندازی دوباره دستگاه دوباره وصل می‌شود."
  },
  kill_switch_android_system_title: {
    en: "System kill switch", es: "Interruptor de seguridad del sistema", fr: "Coupure de sécurité du système",
    ru: "Системное аварийное отключение", uk: "Системне аварійне відключення", zh: "系统断网保护",
    ar: "مفتاح الإيقاف في النظام", fa: "کلید قطع اضطراری سیستم"
  },
  // The names Android gives these two switches in its VPN settings.
  kill_switch_android_always_on_vpn: {
    en: "Always-on VPN", es: "VPN siempre activada", fr: "VPN permanent", ru: "Постоянная VPN",
    uk: "Постійна VPN", zh: "始终开启的 VPN", ar: "شبكة VPN الدائمة التشغيل", fa: "VPN همیشه روشن"
  },
  kill_switch_android_block_without_vpn: {
    en: "Block connections without VPN", es: "Bloquear conexiones sin VPN", fr: "Bloquer les connexions sans VPN",
    ru: "Блокировать соединения без VPN", uk: "Блокувати з'єднання без VPN", zh: "屏蔽未使用 VPN 的所有连接",
    ar: "حظر الاتصالات بدون VPN", fa: "مسدود کردن اتصال‌های بدون VPN"
  },
  kill_switch_android_system_body: {
    en: "Android itself can block every connection outside PangeaVPN, even when the VPN is off. To turn this on, open VPN settings, tap the gear next to PangeaVPN, then turn on Always-on VPN and Block connections without VPN.",
    es: "Android puede bloquear por sí mismo cualquier conexión fuera de PangeaVPN, incluso cuando la VPN está desactivada. Para activarlo, abre los ajustes de VPN, toca el engranaje junto a PangeaVPN y activa «VPN siempre activada» y «Bloquear conexiones sin VPN».",
    fr: "Android peut lui-même bloquer toute connexion hors de PangeaVPN, même quand le VPN est coupé. Pour l'activer, ouvrez les paramètres VPN, touchez la roue dentée à côté de PangeaVPN, puis activez « VPN permanent » et « Bloquer les connexions sans VPN ».",
    ru: "Android может сам блокировать любые соединения в обход PangeaVPN, даже когда VPN выключен. Чтобы включить это, откройте настройки VPN, нажмите на шестерёнку рядом с PangeaVPN и включите «Постоянная VPN» и «Блокировать соединения без VPN».",
    uk: "Android може сам блокувати будь-які з'єднання повз PangeaVPN, навіть коли VPN вимкнено. Щоб увімкнути це, відкрийте налаштування VPN, торкніться шестірні поруч із PangeaVPN і увімкніть «Постійна VPN» та «Блокувати з'єднання без VPN».",
    zh: "Android 本身可以拦截所有不经过 PangeaVPN 的连接，即使 VPN 已关闭也是如此。要开启此功能，请打开 VPN 设置，点按 PangeaVPN 旁边的齿轮图标，然后开启「始终开启的 VPN」和「屏蔽未使用 VPN 的所有连接」。",
    ar: "يستطيع Android نفسه حظر أي اتصال خارج PangeaVPN، حتى عندما تكون VPN متوقفة. لتفعيل ذلك، افتح إعدادات VPN، واضغط على رمز الترس بجانب PangeaVPN، ثم فعّل «شبكة VPN الدائمة التشغيل» و«حظر الاتصالات بدون VPN».",
    fa: "خود Android می‌تواند هر اتصالی را که از PangeaVPN نمی‌گذرد مسدود کند، حتی وقتی VPN خاموش است. برای روشن کردن آن، تنظیمات VPN را باز کنید، روی چرخ‌دندهٔ کنار PangeaVPN بزنید و «VPN همیشه روشن» و «مسدود کردن اتصال‌های بدون VPN» را روشن کنید."
  },
  kill_switch_android_status_unknown: {
    en: "Their current state shows here while PangeaVPN is connected.",
    es: "Su estado actual se muestra aquí mientras PangeaVPN está conectada.",
    fr: "Leur état actuel s'affiche ici pendant que PangeaVPN est connecté.",
    ru: "Их текущее состояние отображается здесь, пока PangeaVPN подключён.",
    uk: "Їхній поточний стан показано тут, поки PangeaVPN підключено.",
    zh: "PangeaVPN 连接期间，这里会显示它们当前的状态。",
    ar: "تظهر حالتهما الحالية هنا أثناء اتصال PangeaVPN.",
    fa: "وضعیت فعلی آن‌ها هنگام اتصال PangeaVPN اینجا نمایش داده می‌شود."
  },
  kill_switch_android_lockdown_note: {
    en: "Lockdown, under Startup, keeps the VPN on from inside PangeaVPN and hides the Disconnect button.",
    es: "El modo bloqueo, en Inicio, mantiene la VPN activa desde PangeaVPN y oculta el botón Desconectar.",
    fr: "Le verrouillage, dans Démarrage, garde le VPN actif depuis PangeaVPN et masque le bouton Se déconnecter.",
    ru: "Lockdown в разделе «Автозапуск» держит VPN включённым из самого PangeaVPN и скрывает кнопку «Отключить».",
    uk: "Блокування в розділі «Автозапуск» тримає VPN увімкненим із самого PangeaVPN і приховує кнопку «Відключитися».",
    zh: "「启动」中的锁定模式会在 PangeaVPN 内让 VPN 保持开启，并隐藏「断开连接」按钮。",
    ar: "يُبقي الإغلاق التام، في «بدء التشغيل»، شبكة VPN مفعّلة من داخل PangeaVPN ويُخفي زر قطع الاتصال.",
    fa: "قفل کامل در بخش «راه‌اندازی»، VPN را از درون PangeaVPN روشن نگه می‌دارد و دکمه «قطع اتصال» را پنهان می‌کند."
  },

  startup_android_auto_connect_note: {
    en: "While it's on, PangeaVPN connects again after your device restarts, as long as it still has permission to run a VPN. Turning the VPN off in system settings stops it until you connect in PangeaVPN again.",
    es: "Mientras está activada, PangeaVPN vuelve a conectarse después de reiniciar el dispositivo, siempre que siga teniendo permiso para usar una VPN. Si desactivas la VPN en los ajustes del sistema, se detiene hasta que vuelvas a conectarte en PangeaVPN.",
    fr: "Tant qu'elle est activée, PangeaVPN se reconnecte après le redémarrage de votre appareil, s'il a toujours l'autorisation d'utiliser un VPN. Couper le VPN dans les paramètres du système l'arrête jusqu'à votre prochaine connexion dans PangeaVPN.",
    ru: "Пока оно включено, PangeaVPN снова подключается после перезагрузки устройства, если у него по-прежнему есть разрешение на VPN. Если выключить VPN в настройках системы, он остановится, пока вы снова не подключитесь в PangeaVPN.",
    uk: "Поки воно увімкнене, PangeaVPN знову підключається після перезавантаження пристрою, якщо в нього досі є дозвіл на VPN. Якщо вимкнути VPN у налаштуваннях системи, він зупиниться, доки ви знову не підключитеся в PangeaVPN.",
    zh: "开启期间，只要 PangeaVPN 仍有使用 VPN 的权限，设备重新启动后它就会重新连接。如果在系统设置中关闭 VPN，它会停止，直到您在 PangeaVPN 中再次连接。",
    ar: "أثناء تفعيله، يعيد PangeaVPN الاتصال بعد إعادة تشغيل جهازك، ما دام لا يزال يملك إذن تشغيل VPN. إيقاف VPN من إعدادات النظام يوقفه حتى تتصل مجددًا من PangeaVPN.",
    fa: "تا وقتی روشن است، PangeaVPN پس از راه‌اندازی دوباره دستگاه دوباره وصل می‌شود، به شرطی که هنوز اجازه اجرای VPN را داشته باشد. خاموش کردن VPN در تنظیمات سیستم آن را تا اتصال دوباره در PangeaVPN متوقف می‌کند."
  },

  notifications_android_denied: {
    en: "Notifications for PangeaVPN are turned off, so alerts can't be shown.",
    es: "Las notificaciones de PangeaVPN están desactivadas, así que no se pueden mostrar alertas.",
    fr: "Les notifications de PangeaVPN sont désactivées : les alertes ne peuvent pas s'afficher.",
    ru: "Уведомления PangeaVPN выключены, поэтому оповещения не показываются.",
    uk: "Сповіщення PangeaVPN вимкнено, тож оповіщення не показуються.",
    zh: "PangeaVPN 的通知已关闭，因此无法显示提醒。",
    ar: "إشعارات PangeaVPN معطّلة، لذا لا يمكن عرض التنبيهات.",
    fa: "اعلان‌های PangeaVPN خاموش است، پس هشدارها نمایش داده نمی‌شوند."
  },
  notifications_android_open_settings: {
    en: "Open Settings", es: "Abrir Ajustes", fr: "Ouvrir les paramètres", ru: "Открыть настройки",
    uk: "Відкрити налаштування", zh: "打开设置", ar: "فتح الإعدادات", fa: "باز کردن تنظیمات"
  },
  notifications_android_open_failed: {
    en: "Couldn't open Settings. Allow notifications for PangeaVPN in your device's Settings app.",
    es: "No se pudieron abrir los Ajustes. Permite las notificaciones de PangeaVPN en la app Ajustes del dispositivo.",
    fr: "Impossible d'ouvrir les paramètres. Autorisez les notifications de PangeaVPN dans l'application Paramètres de votre appareil.",
    ru: "Не удалось открыть настройки. Разрешите уведомления PangeaVPN в приложении «Настройки» на устройстве.",
    uk: "Не вдалося відкрити налаштування. Дозвольте сповіщення PangeaVPN у застосунку «Налаштування» на пристрої.",
    zh: "无法打开设置。请在设备的“设置”应用中允许 PangeaVPN 发送通知。",
    ar: "تعذّر فتح الإعدادات. اسمح بإشعارات PangeaVPN من تطبيق الإعدادات في جهازك.",
    fa: "باز کردن تنظیمات ممکن نشد. در برنامه تنظیمات دستگاه، اعلان‌های PangeaVPN را مجاز کنید."
  },

  // Desktop's wording for following the system language.
  language_android_system: {
    en: "System default", es: "Predeterminado del sistema", fr: "Par défaut du système", ru: "Как в системе",
    uk: "Як у системі", zh: "系统默认", ar: "الافتراضي للنظام", fa: "پیش‌فرض سیستم"
  }
};
