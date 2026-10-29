// Connection alerts the tunnel posts; titles and bodies follow desktop's tray notifications.
export default {
  alerts_connected_title: {
    en: "Connected", es: "Conectado", fr: "Connecté", ru: "Подключено",
    uk: "Підключено", zh: "已连接", ar: "متصل", fa: "متصل شد"
  },
  alerts_connected_body: {
    en: "Your traffic is now protected.",
    es: "Tu tráfico ahora está protegido.",
    fr: "Votre trafic est désormais protégé.",
    ru: "Ваш трафик теперь защищён.",
    uk: "Ваш трафік тепер захищено.",
    zh: "您的流量现已受到保护。",
    ar: "حركة بياناتك محمية الآن.",
    fa: "ترافیک شما اکنون محافظت می‌شود."
  },
  alerts_lost_title: {
    en: "Connection lost", es: "Conexión perdida", fr: "Connexion perdue", ru: "Соединение потеряно",
    uk: "З'єднання втрачено", zh: "连接已断开", ar: "انقطع الاتصال", fa: "اتصال قطع شد"
  },
  alerts_lost_body: {
    en: "PangeaVPN is reconnecting…",
    es: "PangeaVPN se está reconectando…",
    fr: "PangeaVPN se reconnecte…",
    ru: "PangeaVPN переподключается…",
    uk: "PangeaVPN перепідключається…",
    zh: "PangeaVPN 正在重新连接…",
    ar: "يعيد PangeaVPN الاتصال…",
    fa: "PangeaVPN در حال اتصال مجدد است…"
  },
  alerts_restored_title: {
    en: "Back online", es: "De nuevo en línea", fr: "De retour en ligne", ru: "Снова в сети",
    uk: "Знову в мережі", zh: "已恢复在线", ar: "عاد الاتصال", fa: "دوباره آنلاین"
  },
  alerts_restored_body: {
    en: "Your VPN connection is restored.",
    es: "La conexión VPN se ha restablecido.",
    fr: "La connexion VPN est rétablie.",
    ru: "VPN-соединение восстановлено.",
    uk: "VPN-з'єднання відновлено.",
    zh: "VPN 连接已恢复。",
    ar: "تمت استعادة اتصال VPN.",
    fa: "اتصال VPN بازیابی شد."
  },
  alerts_blocked_title: {
    en: "Internet paused to protect you",
    es: "Internet en pausa para protegerte",
    fr: "Internet en pause pour vous protéger",
    ru: "Интернет приостановлен для вашей защиты",
    uk: "Інтернет призупинено для вашого захисту",
    zh: "已暂停网络以保护您",
    ar: "تم إيقاف الإنترنت مؤقتًا لحمايتك",
    fa: "اینترنت برای محافظت از شما موقتاً متوقف شد"
  },
  // %@ is the reason the tunnel stopped, a full sentence from the tunnel errors.
  alerts_blocked_body: {
    en: "%@ The kill switch is blocking traffic while PangeaVPN retries. Open the app and tap Disconnect to go online without VPN.",
    es: "%@ El interruptor de seguridad está bloqueando el tráfico mientras PangeaVPN lo vuelve a intentar. Abre la app y toca Desconectar para navegar sin VPN.",
    fr: "%@ La coupure de sécurité bloque le trafic pendant que PangeaVPN réessaie. Ouvrez l'app et touchez Se déconnecter pour naviguer sans VPN.",
    ru: "%@ Аварийное отключение блокирует трафик, пока PangeaVPN пытается переподключиться. Откройте приложение и нажмите «Отключить», чтобы выйти в сеть без VPN.",
    uk: "%@ Аварійне відключення блокує трафік, поки PangeaVPN намагається перепідключитися. Відкрийте застосунок і натисніть «Відключитися», щоб вийти в мережу без VPN.",
    zh: "%@断网保护正在拦截流量，PangeaVPN 正在重试。打开应用并点击「断开连接」即可在无 VPN 的情况下上网。",
    ar: "%@ يحجب مفتاح الإيقاف حركة البيانات بينما يعيد PangeaVPN المحاولة. افتح التطبيق واضغط على قطع الاتصال للاتصال بالإنترنت بدون VPN.",
    fa: "%@ کلید قطع اضطراری ترافیک را مسدود می‌کند، در حالی که PangeaVPN دوباره تلاش می‌کند. برنامه را باز کنید و «قطع اتصال» را بزنید تا بدون VPN آنلاین شوید."
  }
};
