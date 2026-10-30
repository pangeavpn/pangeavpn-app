// Kill Switch, Startup, Split Tunnelling and Notifications pages; desktop wording reused where it fits.
export default {
  kill_switch_description: {
    en: "How PangeaVPN keeps your traffic from leaking outside the VPN.",
    es: "Cómo evita PangeaVPN que tu tráfico se filtre fuera de la VPN.",
    fr: "Comment PangeaVPN empêche votre trafic de fuiter hors du VPN.",
    ru: "Как PangeaVPN не даёт вашему трафику утечь мимо VPN.",
    uk: "Як PangeaVPN не дає вашому трафіку витекти повз VPN.",
    zh: "PangeaVPN 如何防止您的流量泄漏到 VPN 之外。",
    ar: "كيف يمنع PangeaVPN تسرّب حركة بياناتك خارج VPN.",
    fa: "PangeaVPN چگونه از نشت ترافیک شما به بیرون از VPN جلوگیری می‌کند."
  },
  kill_switch_always_on: {
    en: "Always on", es: "Siempre activo", fr: "Toujours actif", ru: "Всегда включено",
    uk: "Завжди увімкнено", zh: "始终开启", ar: "مفعّل دائمًا", fa: "همیشه روشن"
  },
  kill_switch_always_held: {
    en: "Traffic is held inside the VPN while it reconnects or switches servers.",
    es: "El tráfico se retiene dentro de la VPN mientras se reconecta o cambia de servidor.",
    fr: "Le trafic reste retenu dans le VPN pendant qu'il se reconnecte ou change de serveur.",
    ru: "Трафик удерживается внутри VPN, пока он переподключается или меняет сервер.",
    uk: "Трафік утримується всередині VPN, поки він перепідключається або змінює сервер.",
    zh: "VPN 重新连接或切换服务器时，流量会被留在 VPN 内。",
    ar: "تبقى حركة البيانات محتجزة داخل VPN أثناء إعادة الاتصال أو تبديل الخادم.",
    fa: "ترافیک هنگام اتصال دوباره یا تعویض سرور، درون VPN نگه داشته می‌شود."
  },
  kill_switch_always_routes: {
    en: "All traffic is routed into the VPN, and IPv6 is blocked so it can't leak.",
    es: "Todo el tráfico se envía por la VPN, y el IPv6 se bloquea para que no se filtre.",
    fr: "Tout le trafic passe par le VPN, et l'IPv6 est bloqué pour qu'il ne fuie pas.",
    ru: "Весь трафик направляется в VPN, а IPv6 блокируется, чтобы он не утекал.",
    uk: "Увесь трафік спрямовується у VPN, а IPv6 блокується, щоб він не витікав.",
    zh: "所有流量都经由 VPN 传输，IPv6 会被拦截，防止泄露。",
    ar: "تُوجَّه كل حركة البيانات عبر VPN، ويُحظر IPv6 حتى لا يتسرّب.",
    fa: "همهٔ ترافیک از راه VPN فرستاده می‌شود و IPv6 مسدود است تا نشت نکند."
  },
  kill_switch_always_restart: {
    en: "If iOS stops the VPN, it restarts itself when Auto-connect is on.",
    es: "Si iOS detiene la VPN, se reinicia sola cuando la conexión automática está activada.",
    fr: "Si iOS arrête le VPN, il redémarre de lui-même quand la connexion automatique est activée.",
    ru: "Если iOS остановит VPN, он перезапустится сам, когда включено автоподключение.",
    uk: "Якщо iOS зупинить VPN, він перезапуститься сам, коли увімкнено автопідключення.",
    zh: "开启自动连接时，即使 iOS 停止了 VPN，它也会自行重新启动。",
    ar: "إذا أوقف iOS شبكة VPN، فإنها تعيد تشغيل نفسها عندما يكون الاتصال التلقائي مفعّلًا.",
    fa: "اگر iOS اتصال VPN را متوقف کند، وقتی اتصال خودکار روشن است خودش دوباره راه می‌افتد."
  },
  kill_switch_strict_title: {
    en: "Strict kill switch", es: "Interruptor de seguridad estricto", fr: "Coupure de sécurité stricte",
    ru: "Строгое аварийное отключение", uk: "Суворе аварійне відключення", zh: "严格断网保护",
    ar: "مفتاح الإيقاف الصارم", fa: "کلید قطع اضطراری سخت‌گیرانه"
  },
  kill_switch_strict_hint: {
    en: "Sends every connection through the VPN, including ones iOS would otherwise let out. Applies on next connect.",
    es: "Envía todas las conexiones por la VPN, incluidas las que iOS dejaría salir de otro modo. Se aplica en la próxima conexión.",
    fr: "Fait passer toutes les connexions par le VPN, y compris celles qu'iOS laisserait sortir autrement. Effectif à la prochaine connexion.",
    ru: "Направляет через VPN все соединения, включая те, которые iOS иначе выпустила бы напрямую. Действует со следующего подключения.",
    uk: "Спрямовує через VPN усі з'єднання, зокрема ті, які iOS інакше випустила б напряму. Діє з наступного підключення.",
    zh: "让所有连接都经过 VPN，包括 iOS 原本会放行到 VPN 之外的连接。下次连接时生效。",
    ar: "يمرّر كل اتصال عبر VPN، بما في ذلك الاتصالات التي كان iOS سيسمح بخروجها مباشرة. يسري عند الاتصال التالي.",
    fa: "همه اتصال‌ها را از VPN عبور می‌دهد، از جمله آن‌هایی که iOS در غیر این صورت مستقیم بیرون می‌فرستد. از اتصال بعدی اعمال می‌شود."
  },
  kill_switch_strict_confirm_title: {
    en: "Turn on strict kill switch?",
    es: "¿Activar el interruptor de seguridad estricto?",
    fr: "Activer la coupure de sécurité stricte ?",
    ru: "Включить строгое аварийное отключение?",
    uk: "Увімкнути суворе аварійне відключення?",
    zh: "要开启严格断网保护吗？",
    ar: "هل تريد تفعيل مفتاح الإيقاف الصارم؟",
    fa: "کلید قطع اضطراری سخت‌گیرانه روشن شود؟"
  },
  kill_switch_strict_confirm_on: {
    en: "Turn On", es: "Activar", fr: "Activer", ru: "Включить",
    uk: "Увімкнути", zh: "开启", ar: "تفعيل", fa: "روشن کردن"
  },
  kill_switch_strict_confirm_cancel: {
    en: "Cancel", es: "Cancelar", fr: "Annuler", ru: "Отмена",
    uk: "Скасувати", zh: "取消", ar: "إلغاء", fa: "لغو"
  },
  kill_switch_caveat_update: {
    en: "Updating PangeaVPN while connected can leave your iPhone offline until it restarts.",
    es: "Actualizar PangeaVPN mientras está conectada puede dejar tu iPhone sin conexión hasta que se reinicie.",
    fr: "Mettre à jour PangeaVPN pendant qu'il est connecté peut laisser votre iPhone hors ligne jusqu'à son redémarrage.",
    ru: "Обновление PangeaVPN во время подключения может оставить iPhone без сети до его перезагрузки.",
    uk: "Оновлення PangeaVPN під час підключення може залишити iPhone без мережі до його перезавантаження.",
    zh: "在连接状态下更新 PangeaVPN，可能导致 iPhone 断网，直到重新启动。",
    ar: "قد يؤدي تحديث PangeaVPN أثناء الاتصال إلى بقاء iPhone بلا إنترنت حتى تعيد تشغيله.",
    fa: "به‌روزرسانی PangeaVPN در حالت متصل ممکن است iPhone را تا راه‌اندازی دوباره بدون اینترنت بگذارد."
  },
  kill_switch_caveat_lan: {
    en: "AirDrop, AirPlay and printing need Allow LAN.",
    es: "AirDrop, AirPlay y la impresión necesitan Permitir LAN.",
    fr: "AirDrop, AirPlay et l'impression nécessitent Autoriser le LAN.",
    ru: "Для AirDrop, AirPlay и печати нужно включить «Разрешить LAN».",
    uk: "Для AirDrop, AirPlay і друку потрібно увімкнути «Дозволити LAN».",
    zh: "AirDrop、AirPlay 和打印需要开启「允许 LAN」。",
    ar: "تحتاج AirDrop وAirPlay والطباعة إلى «السماح بشبكة LAN».",
    fa: "AirDrop، AirPlay و چاپ به «اجازه به LAN» نیاز دارند."
  },
  kill_switch_caveat_carplay: {
    en: "CarPlay and Personal Hotspot may not work.",
    es: "Puede que CarPlay y el Punto de acceso personal no funcionen.",
    fr: "CarPlay et le Partage de connexion peuvent ne pas fonctionner.",
    ru: "CarPlay и режим модема могут не работать.",
    uk: "CarPlay і режим модема можуть не працювати.",
    zh: "CarPlay 和个人热点可能无法使用。",
    ar: "قد لا تعمل CarPlay ونقطة الاتصال الشخصية.",
    fa: "ممکن است CarPlay و هات‌اسپات شخصی کار نکنند."
  },
  kill_switch_caveat_system: {
    en: "Some Apple system services may still bypass the VPN.",
    es: "Algunos servicios del sistema de Apple pueden seguir evitando la VPN.",
    fr: "Certains services système d'Apple peuvent encore contourner le VPN.",
    ru: "Некоторые системные службы Apple всё равно могут идти в обход VPN.",
    uk: "Деякі системні служби Apple усе одно можуть обходити VPN.",
    zh: "部分 Apple 系统服务仍可能绕过 VPN。",
    ar: "قد تظل بعض خدمات نظام Apple تتجاوز VPN.",
    fa: "برخی سرویس‌های سیستمی Apple ممکن است همچنان VPN را دور بزنند."
  },
  kill_switch_toast_on: {
    en: "Strict kill switch on. Applies on next connect.",
    es: "Interruptor de seguridad estricto activado. Se aplica en la próxima conexión.",
    fr: "Coupure de sécurité stricte activée. Effectif à la prochaine connexion.",
    ru: "Строгое аварийное отключение включено. Действует со следующего подключения.",
    uk: "Суворе аварійне відключення увімкнено. Діє з наступного підключення.",
    zh: "严格断网保护已开启。下次连接时生效。",
    ar: "تم تفعيل مفتاح الإيقاف الصارم. يسري عند الاتصال التالي.",
    fa: "کلید قطع اضطراری سخت‌گیرانه روشن شد. از اتصال بعدی اعمال می‌شود."
  },
  kill_switch_toast_off: {
    en: "Strict kill switch off. Applies on next connect.",
    es: "Interruptor de seguridad estricto desactivado. Se aplica en la próxima conexión.",
    fr: "Coupure de sécurité stricte désactivée. Effectif à la prochaine connexion.",
    ru: "Строгое аварийное отключение выключено. Действует со следующего подключения.",
    uk: "Суворе аварійне відключення вимкнено. Діє з наступного підключення.",
    zh: "严格断网保护已关闭。下次连接时生效。",
    ar: "تم إيقاف مفتاح الإيقاف الصارم. يسري عند الاتصال التالي.",
    fa: "کلید قطع اضطراری سخت‌گیرانه خاموش شد. از اتصال بعدی اعمال می‌شود."
  },
  kill_switch_summary_strict: {
    en: "Strict", es: "Estricto", fr: "Strict", ru: "Строгий",
    uk: "Суворий", zh: "严格", ar: "صارم", fa: "سخت‌گیرانه"
  },

  startup_description: {
    en: "Automatic connection and reconnection.",
    es: "Conexión y reconexión automáticas.",
    fr: "Connexion et reconnexion automatiques.",
    ru: "Автоматическое подключение и переподключение.",
    uk: "Автоматичне підключення й перепідключення.",
    zh: "自动连接与重新连接。",
    ar: "الاتصال وإعادة الاتصال التلقائيان.",
    fa: "اتصال و اتصال دوباره خودکار."
  },
  startup_auto_connect_title: {
    en: "Auto-connect", es: "Conexión automática", fr: "Connexion automatique", ru: "Автоподключение",
    uk: "Автопідключення", zh: "自动连接", ar: "الاتصال التلقائي", fa: "اتصال خودکار"
  },
  startup_auto_connect_hint: {
    en: "Connects when your iPhone starts or the network returns, and reconnects if the tunnel drops. Disconnecting in PangeaVPN pauses it until you connect again.",
    es: "Se conecta cuando tu iPhone se enciende o vuelve la red, y vuelve a conectar si el túnel se cae. Si te desconectas en PangeaVPN, se pausa hasta que vuelvas a conectar.",
    fr: "Se connecte au démarrage de votre iPhone ou au retour du réseau, et se reconnecte si le tunnel tombe. Se déconnecter dans PangeaVPN le met en pause jusqu'à votre prochaine connexion.",
    ru: "Подключается при включении iPhone или возвращении сети и переподключается, если туннель обрывается. Если отключиться в PangeaVPN, ждёт, пока вы подключитесь снова.",
    uk: "Підключається під час увімкнення iPhone або повернення мережі й перепідключається, якщо тунель обірветься. Якщо відключитися в PangeaVPN, чекає, поки ви підключитеся знову.",
    zh: "iPhone 启动或网络恢复时自动连接，隧道断开后自动重连。在 PangeaVPN 中断开连接会暂停此功能，直到您再次连接。",
    ar: "يتصل عند تشغيل iPhone أو عودة الشبكة، ويعيد الاتصال إذا انقطع النفق. قطع الاتصال من PangeaVPN يوقفه مؤقتًا حتى تتصل مرة أخرى.",
    fa: "هنگام روشن شدن iPhone یا بازگشت شبکه وصل می‌شود و اگر تونل قطع شود دوباره وصل می‌شود. قطع اتصال در PangeaVPN آن را تا اتصال بعدی متوقف می‌کند."
  },
  startup_auto_connect_note: {
    en: "While it's on, iOS may turn the VPN back on if you switch it off in Control Center or iOS Settings. To stop it, disconnect in PangeaVPN.",
    es: "Mientras está activada, iOS puede volver a activar la VPN si la desactivas en el Centro de control o en Ajustes de iOS. Para detenerla, desconéctate en PangeaVPN.",
    fr: "Tant qu'elle est activée, iOS peut rallumer le VPN si vous le coupez depuis le Centre de contrôle ou les Réglages d'iOS. Pour l'arrêter, déconnectez-vous dans PangeaVPN.",
    ru: "Пока оно включено, iOS может снова включить VPN, если выключить его в Пункте управления или Настройках iOS. Чтобы остановить VPN, отключитесь в PangeaVPN.",
    uk: "Поки воно увімкнене, iOS може знову ввімкнути VPN, якщо вимкнути його в Пункті керування чи Параметрах iOS. Щоб зупинити VPN, відключіться в PangeaVPN.",
    zh: "开启期间，如果你在控制中心或 iOS 设置中关闭 VPN，iOS 可能会重新开启它。要停止 VPN，请在 PangeaVPN 中断开连接。",
    ar: "أثناء تفعيله، قد يعيد iOS تشغيل VPN إذا أوقفته من مركز التحكم أو إعدادات iOS. لإيقافه، اقطع الاتصال من PangeaVPN.",
    fa: "تا وقتی روشن است، اگر VPN را از مرکز کنترل یا تنظیمات iOS خاموش کنید، ممکن است iOS دوباره روشنش کند. برای توقف، در PangeaVPN قطع اتصال کنید."
  },
  startup_lockdown_title: {
    en: "Lockdown", es: "Modo bloqueo", fr: "Verrouillage", ru: "Lockdown",
    uk: "Блокування", zh: "锁定模式", ar: "الإغلاق التام", fa: "قفل کامل"
  },
  startup_lockdown_hint: {
    en: "Keeps the VPN on at all times. The Disconnect button is hidden until you turn Lockdown off.",
    es: "Mantiene la VPN activa en todo momento. El botón Desconectar se oculta hasta que desactives el modo bloqueo.",
    fr: "Garde le VPN actif en permanence. Le bouton Se déconnecter est masqué tant que vous ne désactivez pas le verrouillage.",
    ru: "Держит VPN включённым всегда. Кнопка «Отключить» скрыта, пока вы не выключите Lockdown.",
    uk: "Тримає VPN увімкненим завжди. Кнопку «Відключитися» приховано, доки ви не вимкнете блокування.",
    zh: "让 VPN 始终保持开启。在您关闭锁定模式之前，「断开连接」按钮会被隐藏。",
    ar: "يبقي VPN مفعّلًا في كل الأوقات. يُخفى زر قطع الاتصال حتى توقف الإغلاق التام.",
    fa: "VPN را همیشه روشن نگه می‌دارد. دکمه «قطع اتصال» تا وقتی قفل کامل را خاموش نکنید پنهان است."
  },
  startup_lockdown_keeps_auto: {
    en: "Lockdown keeps Auto-connect on.",
    es: "El modo bloqueo mantiene activada la conexión automática.",
    fr: "Le verrouillage garde la connexion automatique activée.",
    ru: "Lockdown держит автоподключение включённым.",
    uk: "Блокування тримає автопідключення увімкненим.",
    zh: "锁定模式会让自动连接保持开启。",
    ar: "يبقي الإغلاق التام الاتصال التلقائي مفعّلًا.",
    fa: "قفل کامل اتصال خودکار را روشن نگه می‌دارد."
  },
  startup_toast_auto_on: {
    en: "Auto-connect on. PangeaVPN reconnects after drops and when the network returns.",
    es: "Conexión automática activada. PangeaVPN se reconecta tras una caída y cuando vuelve la red.",
    fr: "Connexion automatique activée. PangeaVPN se reconnecte après une coupure et au retour du réseau.",
    ru: "Автоподключение включено. PangeaVPN переподключается после обрывов и при возвращении сети.",
    uk: "Автопідключення увімкнено. PangeaVPN перепідключається після обривів і коли повертається мережа.",
    zh: "自动连接已开启。PangeaVPN 会在断线后和网络恢复时重新连接。",
    ar: "الاتصال التلقائي مفعّل. يعيد PangeaVPN الاتصال بعد الانقطاع وعند عودة الشبكة.",
    fa: "اتصال خودکار روشن شد. PangeaVPN پس از قطعی و هنگام بازگشت شبکه دوباره وصل می‌شود."
  },
  startup_toast_auto_off: {
    en: "Auto-connect off.", es: "Conexión automática desactivada.", fr: "Connexion automatique désactivée.",
    ru: "Автоподключение выключено.", uk: "Автопідключення вимкнено.", zh: "自动连接已关闭。",
    ar: "الاتصال التلقائي متوقف.", fa: "اتصال خودکار خاموش شد."
  },
  startup_toast_lockdown_on: {
    en: "Lockdown on. The VPN stays on until you turn Lockdown off.",
    es: "Modo bloqueo activado. La VPN permanece activa hasta que lo desactives.",
    fr: "Verrouillage activé. Le VPN reste actif jusqu'à ce que vous le désactiviez.",
    ru: "Lockdown включён. VPN остаётся включённым, пока вы не выключите Lockdown.",
    uk: "Блокування увімкнено. VPN залишається увімкненим, доки ви не вимкнете блокування.",
    zh: "锁定模式已开启。在您关闭锁定模式之前，VPN 将保持开启。",
    ar: "الإغلاق التام مفعّل. يبقى VPN مفعّلًا حتى توقف الإغلاق التام.",
    fa: "قفل کامل روشن شد. VPN تا وقتی قفل کامل را خاموش نکنید روشن می‌ماند."
  },
  startup_toast_lockdown_off: {
    en: "Lockdown off. You can disconnect again.",
    es: "Modo bloqueo desactivado. Ya puedes desconectarte.",
    fr: "Verrouillage désactivé. Vous pouvez de nouveau vous déconnecter.",
    ru: "Lockdown выключен. Теперь можно снова отключаться.",
    uk: "Блокування вимкнено. Тепер можна знову відключатися.",
    zh: "锁定模式已关闭。您可以再次断开连接。",
    ar: "الإغلاق التام متوقف. يمكنك قطع الاتصال مجددًا.",
    fa: "قفل کامل خاموش شد. دوباره می‌توانید اتصال را قطع کنید."
  },

  split_tunnel_description: {
    en: "Addresses listed here bypass the VPN and use your normal connection.",
    es: "Las direcciones indicadas aquí omiten la VPN y usan tu conexión normal.",
    fr: "Les adresses indiquées ici contournent le VPN et utilisent votre connexion normale.",
    ru: "Адреса, указанные здесь, идут в обход VPN через обычное соединение.",
    uk: "Адреси, вказані тут, обходять VPN і використовують звичайне з'єднання.",
    zh: "此处列出的地址将绕过 VPN，使用您的普通网络连接。",
    ar: "العناوين المدرجة هنا تتجاوز VPN وتستخدم اتصالك العادي.",
    fa: "نشانی‌هایی که اینجا فهرست شده‌اند از VPN عبور نمی‌کنند و از اتصال عادی شما استفاده می‌کنند."
  },
  split_tunnel_note_strict: {
    en: "Strict kill switch is on, so iOS sends everything through the VPN and these ranges are ignored.",
    es: "El interruptor de seguridad estricto está activado, así que iOS envía todo por la VPN y estos rangos se ignoran.",
    fr: "La coupure de sécurité stricte est activée : iOS fait tout passer par le VPN et ces plages sont ignorées.",
    ru: "Включено строгое аварийное отключение, поэтому iOS направляет всё через VPN и эти диапазоны игнорируются.",
    uk: "Увімкнено суворе аварійне відключення, тому iOS спрямовує все через VPN і ці діапазони ігноруються.",
    zh: "严格断网保护已开启，iOS 会让所有流量都经过 VPN，这些范围将被忽略。",
    ar: "مفتاح الإيقاف الصارم مفعّل، لذا يمرّر iOS كل شيء عبر VPN وتُتجاهل هذه النطاقات.",
    fa: "کلید قطع اضطراری سخت‌گیرانه روشن است، پس iOS همه چیز را از VPN می‌فرستد و این محدوده‌ها نادیده گرفته می‌شوند."
  },
  split_tunnel_enabled_title: {
    en: "Use split tunnelling", es: "Usar túnel dividido", fr: "Utiliser le tunnel divisé",
    ru: "Использовать раздельное туннелирование", uk: "Використовувати роздільне тунелювання",
    zh: "启用分流", ar: "استخدام الأنفاق المقسّمة", fa: "استفاده از تونل تفکیک‌شده"
  },
  split_tunnel_enabled_hint: {
    en: "Off sends everything through the VPN. Your ranges are kept. Applies on next connect.",
    es: "Desactivado, todo pasa por la VPN. Tus rangos se conservan. Se aplica en la próxima conexión.",
    fr: "Désactivé, tout passe par le VPN. Vos plages sont conservées. Effectif à la prochaine connexion.",
    ru: "Если выключено, весь трафик идёт через VPN. Список диапазонов сохраняется. Действует со следующего подключения.",
    uk: "Якщо вимкнено, увесь трафік іде через VPN. Список діапазонів зберігається. Діє з наступного підключення.",
    zh: "关闭时，所有流量都走 VPN。已填写的 IP 范围会保留。下次连接时生效。",
    ar: "عند الإيقاف، تمر كل الحركة عبر VPN. تُحفظ قائمة النطاقات. يسري عند الاتصال التالي.",
    fa: "وقتی خاموش است، همه ترافیک از VPN می‌گذرد. فهرست محدوده‌ها حفظ می‌شود. از اتصال بعدی اعمال می‌شود."
  },
  split_tunnel_ranges_title: {
    en: "Excluded IP ranges", es: "Rangos IP excluidos", fr: "Plages IP exclues", ru: "Исключённые IP-диапазоны",
    uk: "Виключені IP-діапазони", zh: "排除的 IP 范围", ar: "نطاقات IP المستثناة", fa: "محدوده‌های IP مستثنا"
  },
  split_tunnel_ranges_hint: {
    en: "IPv4 addresses or ranges, separated by commas. Applies on next connect.",
    es: "Direcciones o rangos IPv4 separados por comas. Se aplica en la próxima conexión.",
    fr: "Adresses ou plages IPv4 séparées par des virgules. Effectif à la prochaine connexion.",
    ru: "IPv4-адреса или диапазоны через запятую. Действует со следующего подключения.",
    uk: "IPv4-адреси або діапазони через кому. Діє з наступного підключення.",
    zh: "IPv4 地址或范围，用逗号分隔。下次连接时生效。",
    ar: "عناوين أو نطاقات IPv4 مفصولة بفواصل. يسري عند الاتصال التالي.",
    fa: "نشانی‌ها یا محدوده‌های IPv4 که با ویرگول جدا شده‌اند. از اتصال بعدی اعمال می‌شود."
  },
  split_tunnel_ranges_placeholder: {
    en: "192.0.2.0/24, 198.51.100.7", es: "192.0.2.0/24, 198.51.100.7", fr: "192.0.2.0/24, 198.51.100.7",
    ru: "192.0.2.0/24, 198.51.100.7", uk: "192.0.2.0/24, 198.51.100.7", zh: "192.0.2.0/24, 198.51.100.7",
    ar: "192.0.2.0/24, 198.51.100.7", fa: "192.0.2.0/24, 198.51.100.7"
  },
  split_tunnel_note_dns: {
    en: "Name lookups (DNS) still go through the VPN.",
    es: "Las consultas de nombres (DNS) siguen pasando por la VPN.",
    fr: "Les résolutions de noms (DNS) passent toujours par le VPN.",
    ru: "Запросы имён (DNS) по-прежнему идут через VPN.",
    uk: "Запити імен (DNS) і далі йдуть через VPN.",
    zh: "域名解析（DNS）仍通过 VPN。",
    ar: "استعلامات الأسماء (DNS) تظل تمر عبر VPN.",
    fa: "جستجوی نام‌ها (DNS) همچنان از VPN می‌گذرد."
  },
  split_tunnel_note_lan: {
    en: "For devices on your own network, turn on Allow LAN instead.",
    es: "Para dispositivos de tu propia red, activa Permitir LAN.",
    fr: "Pour les appareils de votre propre réseau, activez plutôt Autoriser le LAN.",
    ru: "Для устройств вашей локальной сети лучше включите «Разрешить LAN».",
    uk: "Для пристроїв вашої локальної мережі краще увімкніть «Дозволити LAN».",
    zh: "若是您自己网络中的设备，请改为开启「允许 LAN」。",
    ar: "للأجهزة على شبكتك المحلية، فعّل «السماح بشبكة LAN» بدلًا من ذلك.",
    fa: "برای دستگاه‌های شبکه خودتان، به جای این «اجازه به LAN» را روشن کنید."
  },
  split_tunnel_note_apps: {
    en: "iPhone can't leave out individual apps: iOS only lets a VPN exclude address ranges.",
    es: "El iPhone no puede excluir apps concretas: iOS solo permite que una VPN excluya rangos de direcciones.",
    fr: "L'iPhone ne peut pas exclure des applications précises : iOS ne permet à un VPN d'exclure que des plages d'adresses.",
    ru: "iPhone не умеет исключать отдельные приложения: iOS позволяет VPN исключать только диапазоны адресов.",
    uk: "iPhone не може виключати окремі застосунки: iOS дозволяє VPN виключати лише діапазони адрес.",
    zh: "iPhone 无法单独排除某个应用：iOS 只允许 VPN 排除地址范围。",
    ar: "لا يستطيع iPhone استثناء تطبيقات بعينها: يسمح iOS لشبكة VPN باستثناء نطاقات العناوين فقط.",
    fa: "iPhone نمی‌تواند برنامه‌های جداگانه را مستثنا کند: iOS فقط اجازه می‌دهد VPN محدوده‌های نشانی را مستثنا کند."
  },
  split_tunnel_toast_on: {
    en: "Split tunnelling on. Applies on next connect.",
    es: "Túnel dividido activado. Se aplica en la próxima conexión.",
    fr: "Tunnel divisé activé. Effectif à la prochaine connexion.",
    ru: "Раздельное туннелирование включено. Действует со следующего подключения.",
    uk: "Роздільне тунелювання увімкнено. Діє з наступного підключення.",
    zh: "分流已开启。下次连接时生效。",
    ar: "تم تفعيل الأنفاق المقسّمة. يسري عند الاتصال التالي.",
    fa: "تونل تفکیک‌شده روشن شد. از اتصال بعدی اعمال می‌شود."
  },
  split_tunnel_toast_off: {
    en: "Split tunnelling off. Applies on next connect.",
    es: "Túnel dividido desactivado. Se aplica en la próxima conexión.",
    fr: "Tunnel divisé désactivé. Effectif à la prochaine connexion.",
    ru: "Раздельное туннелирование выключено. Действует со следующего подключения.",
    uk: "Роздільне тунелювання вимкнено. Діє з наступного підключення.",
    zh: "分流已关闭。下次连接时生效。",
    ar: "تم إيقاف الأنفاق المقسّمة. يسري عند الاتصال التالي.",
    fa: "تونل تفکیک‌شده خاموش شد. از اتصال بعدی اعمال می‌شود."
  },
  split_tunnel_toast_saved: {
    en: "Excluded IP ranges saved.",
    es: "Rangos IP excluidos guardados.",
    fr: "Plages IP exclues enregistrées.",
    ru: "Исключённые IP-диапазоны сохранены.",
    uk: "Виключені IP-діапазони збережено.",
    zh: "已保存排除的 IP 范围。",
    ar: "تم حفظ نطاقات IP المستثناة.",
    fa: "محدوده‌های IP مستثنا ذخیره شد."
  },
  split_tunnel_toast_cleared: {
    en: "No IP ranges excluded.", es: "No hay rangos IP excluidos.", fr: "Aucune plage IP exclue.",
    ru: "IP-диапазоны не исключены.", uk: "IP-діапазони не виключено.", zh: "未排除任何 IP 范围。",
    ar: "لا توجد نطاقات IP مستثناة.", fa: "هیچ محدوده IP مستثنا نیست."
  },
  // %@ is the list of entries as typed.
  split_tunnel_error_not_ipv4: {
    en: "Not an IPv4 address or range: %@.",
    es: "No es una dirección o rango IPv4: %@.",
    fr: "Pas une adresse ou plage IPv4 : %@.",
    ru: "Не IPv4-адрес или диапазон: %@.",
    uk: "Не IPv4-адреса чи діапазон: %@.",
    zh: "不是 IPv4 地址或范围：%@。",
    ar: "ليس عنوان IPv4 أو نطاقًا: %@.",
    fa: "نشانی یا محدوده IPv4 نیست: %@."
  },
  split_tunnel_error_too_broad: {
    en: "Too broad, use /8 or narrower: %@.",
    es: "Demasiado amplio, usa /8 o más estrecho: %@.",
    fr: "Trop large, utilisez /8 ou plus étroit : %@.",
    ru: "Слишком широко, используйте /8 или уже: %@.",
    uk: "Надто широко, використовуйте /8 або вужче: %@.",
    zh: "范围太大，请使用 /8 或更小的范围：%@。",
    ar: "واسع جدًا، استخدم /8 أو أضيق: %@.",
    fa: "بیش از حد گسترده است، از /8 یا محدودتر استفاده کنید: %@."
  },
  split_tunnel_error_too_many: {
    en: "Too many ranges. Remove some first.",
    es: "Demasiados rangos. Quita algunos primero.",
    fr: "Trop de plages. Retirez-en d'abord.",
    ru: "Слишком много диапазонов. Сначала уберите часть.",
    uk: "Забагато діапазонів. Спершу приберіть частину.",
    zh: "范围太多。请先移除一些。",
    ar: "نطاقات كثيرة جدًا. أزل بعضها أولًا.",
    fa: "محدوده‌ها بیش از حد زیادند. اول چندتایی را بردارید."
  },
  split_tunnel_summary_empty: {
    en: "Nothing excluded", es: "Nada excluido", fr: "Rien d'exclu", ru: "Ничего не исключено",
    uk: "Нічого не виключено", zh: "未排除任何内容", ar: "لا شيء مستثنى", fa: "چیزی مستثنا نشده"
  },
  split_tunnel_summary_one: {
    en: "1 range", es: "1 rango", fr: "1 plage", ru: "1 диапазон",
    uk: "1 діапазон", zh: "1 个范围", ar: "نطاق واحد", fa: "1 محدوده"
  },
  // Counts start at 2; ru, uk and ar use a label form so one wording fits every count.
  split_tunnel_summary_count: {
    en: "%lld ranges", es: "%lld rangos", fr: "%lld plages", ru: "Диапазонов: %lld",
    uk: "Діапазонів: %lld", zh: "%lld 个范围", ar: "النطاقات: %lld", fa: "%lld محدوده"
  },

  notifications_description: {
    en: "System notifications about your connection.",
    es: "Notificaciones del sistema sobre tu conexión.",
    fr: "Notifications système sur votre connexion.",
    ru: "Системные уведомления о вашем подключении.",
    uk: "Системні сповіщення про ваше підключення.",
    zh: "关于您连接状态的系统通知。",
    ar: "إشعارات النظام حول اتصالك.",
    fa: "اعلان‌های سیستم درباره اتصال شما."
  },
  notifications_alerts_title: {
    en: "Connection alerts", es: "Alertas de conexión", fr: "Alertes de connexion", ru: "Оповещения о подключении",
    uk: "Сповіщення про підключення", zh: "连接提醒", ar: "تنبيهات الاتصال", fa: "هشدارهای اتصال"
  },
  notifications_alerts_hint: {
    en: "Tells you when the VPN drops, comes back, or is blocking traffic.",
    es: "Te avisa cuando la VPN se cae, vuelve o está bloqueando el tráfico.",
    fr: "Vous avertit quand le VPN tombe, revient ou bloque le trafic.",
    ru: "Сообщает, когда VPN теряет связь, возвращается или блокирует трафик.",
    uk: "Повідомляє, коли VPN втрачає зв'язок, повертається або блокує трафік.",
    zh: "在 VPN 断线、恢复或正在拦截流量时通知您。",
    ar: "ينبهك عند انقطاع VPN أو عودته أو حجبه لحركة البيانات.",
    fa: "هنگام افت یا بازگشت VPN، یا وقتی ترافیک را مسدود می‌کند، به شما اطلاع می‌دهد."
  },
  notifications_denied: {
    en: "Notifications for PangeaVPN are off in iOS Settings, so alerts can't be shown.",
    es: "Las notificaciones de PangeaVPN están desactivadas en Ajustes de iOS, así que no se pueden mostrar alertas.",
    fr: "Les notifications de PangeaVPN sont désactivées dans les Réglages d'iOS : les alertes ne peuvent pas s'afficher.",
    ru: "Уведомления PangeaVPN выключены в Настройках iOS, поэтому оповещения не показываются.",
    uk: "Сповіщення PangeaVPN вимкнено в Параметрах iOS, тож оповіщення не показуються.",
    zh: "PangeaVPN 的通知已在 iOS 设置中关闭，因此无法显示提醒。",
    ar: "إشعارات PangeaVPN معطّلة في إعدادات iOS، لذا لا يمكن عرض التنبيهات.",
    fa: "اعلان‌های PangeaVPN در تنظیمات iOS خاموش است، پس هشدارها نمایش داده نمی‌شوند."
  },
  notifications_open_settings: {
    en: "Open Settings", es: "Abrir Ajustes", fr: "Ouvrir Réglages", ru: "Открыть Настройки",
    uk: "Відкрити Параметри", zh: "打开设置", ar: "فتح الإعدادات", fa: "باز کردن تنظیمات"
  },
  notifications_not_asked: {
    en: "PangeaVPN needs your permission to show alerts.",
    es: "PangeaVPN necesita tu permiso para mostrar alertas.",
    fr: "PangeaVPN a besoin de votre autorisation pour afficher des alertes.",
    ru: "PangeaVPN нужно ваше разрешение, чтобы показывать оповещения.",
    uk: "PangeaVPN потрібен ваш дозвіл, щоб показувати сповіщення.",
    zh: "PangeaVPN 需要您的许可才能显示提醒。",
    ar: "يحتاج PangeaVPN إلى إذنك لعرض التنبيهات.",
    fa: "PangeaVPN برای نمایش هشدارها به اجازه شما نیاز دارد."
  },
  notifications_allow: {
    en: "Allow Notifications", es: "Permitir notificaciones", fr: "Autoriser les notifications", ru: "Разрешить уведомления",
    uk: "Дозволити сповіщення", zh: "允许通知", ar: "السماح بالإشعارات", fa: "اجازه دادن به اعلان‌ها"
  },
  notifications_toast_on: {
    en: "Connection alerts on.", es: "Alertas de conexión activadas.", fr: "Alertes de connexion activées.",
    ru: "Оповещения о подключении включены.", uk: "Сповіщення про підключення увімкнено.", zh: "连接提醒已开启。",
    ar: "تنبيهات الاتصال مفعّلة.", fa: "هشدارهای اتصال روشن شد."
  },
  notifications_toast_off: {
    en: "Connection alerts off.", es: "Alertas de conexión desactivadas.", fr: "Alertes de connexion désactivées.",
    ru: "Оповещения о подключении выключены.", uk: "Сповіщення про підключення вимкнено.", zh: "连接提醒已关闭。",
    ar: "تنبيهات الاتصال متوقفة.", fa: "هشدارهای اتصال خاموش شد."
  }
};
