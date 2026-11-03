// Censorship Bypass, Provisioning and Network settings pages; wording follows desktop.
export default {
  bypass_auto_desc: {
    en: "Tries each disguise in turn and remembers what worked on this network.",
    es: "Prueba cada disfraz por turno y recuerda cuál funcionó en esta red.",
    fr: "Essaie chaque déguisement à tour de rôle et retient ce qui a marché sur ce réseau.",
    ru: "Пробует маскировки по очереди и запоминает, что сработало в этой сети.",
    uk: "Пробує маскування по черзі й запам'ятовує, що спрацювало в цій мережі.",
    zh: "依次尝试每种伪装，并记住在这个网络上管用的那个。",
    ar: "يجرّب كل تمويه بالترتيب ويتذكّر ما نجح على هذه الشبكة.",
    fa: "هر استتار را به‌ترتیب امتحان می‌کند و به‌یاد می‌سپارد روی این شبکه چه جواب داد."
  },
  bypass_cloak_desc: {
    en: "TLS aimed at a decoy site, so the traffic looks like an ordinary visit there.",
    es: "TLS dirigido a un sitio señuelo, así el tráfico parece una visita corriente.",
    fr: "TLS visant un site leurre, si bien que le trafic ressemble à une visite ordinaire.",
    ru: "TLS под сайт-прикрытие, так что трафик выглядит как обычный визит туда.",
    uk: "TLS під сайт-прикриття, тож трафік має вигляд звичайного візиту туди.",
    zh: "指向诱饵站点的 TLS，流量看起来就像一次普通访问。",
    ar: "TLS موجَّه إلى موقع خادع، فتبدو الحركة كزيارة عادية له.",
    fa: "TLS به‌سمت یک سایت طعمه، طوری که ترافیک مثل بازدیدی عادی به‌نظر برسد."
  },
  bypass_reality_desc: {
    en: "Borrows a real site's TLS handshake, so probes see a genuine site. The strongest disguise.",
    es: "Toma prestado el saludo TLS de un sitio real, así los sondeos ven un sitio auténtico. El disfraz más sólido.",
    fr: "Emprunte la poignée de main TLS d'un vrai site, si bien que les sondages voient un site authentique. Le déguisement le plus solide.",
    ru: "Заимствует TLS-рукопожатие настоящего сайта, так что зонды видят подлинный сайт. Самая крепкая маскировка.",
    uk: "Позичає TLS-рукостискання справжнього сайта, тож зонди бачать автентичний сайт. Найміцніше маскування.",
    zh: "借用真实网站的 TLS 握手，探测看到的是一个真站点。最强的伪装。",
    ar: "يستعير مصافحة TLS من موقع حقيقي، فيرى الفحص موقعًا أصيلًا. أقوى تمويه.",
    fa: "دست‌دادن TLS یک سایت واقعی را قرض می‌گیرد، پس کاوش‌ها یک سایت اصیل می‌بینند. قوی‌ترین استتار."
  },
  bypass_shadowsocks_desc: {
    en: "No TLS at all, so a block aimed at TLS misses it.",
    es: "Sin nada de TLS, así que un bloqueo dirigido a TLS no lo alcanza.",
    fr: "Aucun TLS du tout, donc un blocage visant TLS le manque.",
    ru: "Никакого TLS, поэтому блокировка, нацеленная на TLS, его не задевает.",
    uk: "Жодного TLS, тож блокування, націлене на TLS, його не зачіпає.",
    zh: "完全不用 TLS，针对 TLS 的封锁碰不到它。",
    ar: "لا TLS إطلاقًا، فلا يطاله حجب موجَّه إلى TLS.",
    fa: "هیچ TLS ندارد، پس مسدودسازیِ متمرکز بر TLS به آن نمی‌رسد."
  },
  bypass_hysteria2_desc: {
    en: "UDP and QUIC instead of TCP. Worth a try when every TCP connection dies.",
    es: "UDP y QUIC en lugar de TCP. Vale la pena si se caen todas las conexiones TCP.",
    fr: "UDP et QUIC au lieu de TCP. À essayer quand toutes les connexions TCP tombent.",
    ru: "UDP и QUIC вместо TCP. Стоит попробовать, когда все TCP-соединения обрываются.",
    uk: "UDP і QUIC замість TCP. Варто спробувати, коли всі TCP-з'єднання обриваються.",
    zh: "用 UDP 和 QUIC 而非 TCP。所有 TCP 连接都断时值得一试。",
    ar: "UDP وQUIC بدل TCP. يستحق التجربة عندما تنقطع كل اتصالات TCP.",
    fa: "UDP و QUIC به‌جای TCP. وقتی همه اتصال‌های TCP می‌افتند، ارزش امتحان دارد."
  },
  bypass_anytls_desc: {
    en: "A padded TLS session, smoothing the TLS-in-TLS shape other TLS transports leak.",
    es: "Una sesión TLS con relleno, que suaviza el patrón TLS-en-TLS que otros transportes TLS dejan ver.",
    fr: "Une session TLS avec remplissage, qui atténue la signature TLS-dans-TLS que d'autres transports TLS laissent voir.",
    ru: "TLS-сессия с заполнением, сглаживающая признак TLS-в-TLS, который выдают другие TLS-транспорты.",
    uk: "TLS-сесія із заповненням, що згладжує ознаку TLS-у-TLS, яку видають інші TLS-транспорти.",
    zh: "带填充的 TLS 会话，抹平其他 TLS 传输暴露的 TLS-in-TLS 特征。",
    ar: "جلسة TLS مبطَّنة، تخفّف بصمة TLS داخل TLS التي تظهرها وسائط TLS الأخرى.",
    fa: "یک نشست TLS با لایه‌گذاری که الگوی TLS-در-TLS را که دیگر ترابری‌های TLS نشان می‌دهند، هموار می‌کند."
  },
  bypass_regions_note: {
    en: "Regions that don't offer the selected method are hidden from the region list.",
    es: "Las regiones que no ofrecen el método elegido se ocultan de la lista de regiones.",
    fr: "Les régions qui ne proposent pas la méthode choisie sont masquées dans la liste des régions.",
    ru: "Регионы без выбранного способа скрыты из списка регионов.",
    uk: "Регіони без вибраного способу приховано зі списку регіонів.",
    zh: "不支持所选方式的地区会从地区列表中隐藏。",
    ar: "تُخفى المناطق التي لا توفّر الطريقة المختارة من قائمة المناطق.",
    fa: "منطقه‌هایی که روش انتخاب‌شده را ندارند، از فهرست منطقه‌ها پنهان می‌شوند."
  },
  bypass_updated: {
    en: "Connection method updated. Reconnect for it to take effect.",
    es: "Método de conexión actualizado. Vuelve a conectar para que surta efecto.",
    fr: "Méthode de connexion mise à jour. Reconnectez-vous pour qu'elle prenne effet.",
    ru: "Способ подключения обновлён. Переподключитесь, чтобы изменение вступило в силу.",
    uk: "Спосіб підключення оновлено. Перепідключіться, щоб зміни набули чинності.",
    zh: "连接方式已更新。请重新连接以生效。",
    ar: "تم تحديث طريقة الاتصال. أعد الاتصال ليسري المفعول.",
    fa: "روش اتصال به‌روزرسانی شد. برای اعمال، دوباره متصل شوید."
  },
  network_update_failed: {
    en: "Failed to update setting.",
    es: "No se pudo actualizar el ajuste.",
    fr: "Échec de la mise à jour du paramètre.",
    ru: "Не удалось обновить настройку.",
    uk: "Не вдалося оновити налаштування.",
    zh: "更新设置失败。",
    ar: "فشل تحديث الإعداد.",
    fa: "به‌روزرسانی تنظیمات ناموفق بود."
  },
  provisioning_active_none: {
    en: "No method in use yet",
    es: "Aún no se usa ningún método",
    fr: "Aucune méthode utilisée pour l’instant",
    ru: "Пока не используется ни один способ",
    uk: "Поки не використовується жоден спосіб",
    zh: "尚未使用任何方式",
    ar: "لا توجد طريقة قيد الاستخدام بعد",
    fa: "هنوز هیچ روشی استفاده نمی‌شود"
  },
  provisioning_active_using: {
    en: "In use: %@",
    es: "En uso: %@",
    fr: "Utilisé : %@",
    ru: "Используется: %@",
    uk: "Використовується: %@",
    zh: "使用中：%@",
    ar: "قيد الاستخدام: %@",
    fa: "در حال استفاده: %@"
  },
  provisioning_active_detail: {
    en: "In use: %@ — %@",
    es: "En uso: %@ — %@",
    fr: "Utilisé : %@ — %@",
    ru: "Используется: %@ — %@",
    uk: "Використовується: %@ — %@",
    zh: "使用中：%@ — %@",
    ar: "قيد الاستخدام: %@ — %@",
    fa: "در حال استفاده: %@ — %@"
  },
  provisioning_in_use: {
    en: "In use", es: "En uso", fr: "Utilisé", ru: "Используется",
    uk: "Використовується", zh: "使用中", ar: "قيد الاستخدام", fa: "در حال استفاده"
  },
  provisioning_test: {
    en: "Test", es: "Probar", fr: "Tester", ru: "Проверить",
    uk: "Перевірити", zh: "测试", ar: "اختبار", fa: "آزمایش"
  },
  provisioning_test_method: {
    en: "Test %@", es: "Probar %@", fr: "Tester %@", ru: "Проверить: %@",
    uk: "Перевірити: %@", zh: "测试 %@", ar: "اختبار %@", fa: "آزمایش %@"
  },
  provisioning_testing: {
    en: "Testing…", es: "Probando…", fr: "Test en cours…", ru: "Проверка…",
    uk: "Перевірка…", zh: "测试中…", ar: "جارٍ الاختبار…", fa: "در حال آزمایش…"
  },
  provisioning_result_ok: {
    en: "Works (%lld ms)",
    es: "Funciona (%lld ms)",
    fr: "Fonctionne (%lld ms)",
    ru: "Работает (%lld мс)",
    uk: "Працює (%lld мс)",
    zh: "可用（%lld 毫秒）",
    ar: "يعمل (%lld مللي ثانية)",
    fa: "کار می‌کند (%lld میلی‌ثانیه)"
  },
  provisioning_result_ok_detail: {
    en: "Works via %@ (%lld ms)",
    es: "Funciona vía %@ (%lld ms)",
    fr: "Fonctionne via %@ (%lld ms)",
    ru: "Работает через %@ (%lld мс)",
    uk: "Працює через %@ (%lld мс)",
    zh: "通过 %@ 可用（%lld 毫秒）",
    ar: "يعمل عبر %@ (%lld مللي ثانية)",
    fa: "از طریق %@ کار می‌کند (%lld میلی‌ثانیه)"
  },
  provisioning_result_fail: {
    en: "Could not reach our servers",
    es: "No se pudo contactar con nuestros servidores",
    fr: "Impossible de joindre nos serveurs",
    ru: "Не удалось связаться с нашими серверами",
    uk: "Не вдалося зв’язатися з нашими серверами",
    zh: "无法连接到我们的服务器",
    ar: "تعذّر الوصول إلى خوادمنا",
    fa: "دسترسی به سرورهای ما ممکن نشد"
  },
  provisioning_result_no_address: {
    en: "No address stored yet, and encrypted DNS is off",
    es: "Aún no hay dirección guardada y el DNS cifrado está desactivado",
    fr: "Aucune adresse enregistrée et le DNS chiffré est désactivé",
    ru: "Адрес ещё не сохранён, а шифрованный DNS выключен",
    uk: "Адресу ще не збережено, а шифрований DNS вимкнено",
    zh: "尚未保存地址，且加密 DNS 已关闭",
    ar: "لا يوجد عنوان محفوظ بعد، وDNS المشفّر مُعطَّل",
    fa: "هنوز نشانی ذخیره نشده و DNS رمزگذاری‌شده خاموش است"
  },
  provisioning_result_no_credentials: {
    en: "No Shadowsocks credentials stored yet",
    es: "Aún no hay credenciales de Shadowsocks guardadas",
    fr: "Aucun identifiant Shadowsocks enregistré",
    ru: "Учётные данные Shadowsocks ещё не сохранены",
    uk: "Облікові дані Shadowsocks ще не збережено",
    zh: "尚未保存 Shadowsocks 凭据",
    ar: "لا توجد بيانات اعتماد Shadowsocks محفوظة بعد",
    fa: "هنوز اعتبارنامه Shadowsocks ذخیره نشده است"
  },
  provisioning_result_no_reality_credentials: {
    en: "No REALITY credentials stored yet",
    es: "Aún no hay credenciales de REALITY guardadas",
    fr: "Aucun identifiant REALITY enregistré",
    ru: "Учётные данные REALITY ещё не сохранены",
    uk: "Облікові дані REALITY ще не збережено",
    zh: "尚未保存 REALITY 凭据",
    ar: "لا توجد بيانات اعتماد REALITY محفوظة بعد",
    fa: "هنوز اعتبارنامه REALITY ذخیره نشده است"
  },
  provisioning_result_no_relay: {
    en: "No relay configured yet",
    es: "Aún no hay ningún relé configurado",
    fr: "Aucun relais configuré",
    ru: "Ретранслятор ещё не настроен",
    uk: "Ретранслятор ще не налаштовано",
    zh: "尚未配置中继",
    ar: "لم يتم إعداد أي مُرحِّل بعد",
    fa: "هنوز هیچ بازپخشی پیکربندی نشده است"
  },
  provisioning_result_busy: {
    en: "Another test is still running",
    es: "Todavía se está ejecutando otra prueba",
    fr: "Un autre test est encore en cours",
    ru: "Другая проверка ещё выполняется",
    uk: "Інша перевірка ще триває",
    zh: "另一项测试仍在进行",
    ar: "ما زال هناك اختبار آخر قيد التشغيل",
    fa: "آزمایش دیگری هنوز در حال اجراست"
  },
  provisioning_method_on: {
    en: "%@ — on", es: "%@ — activado", fr: "%@ — activée", ru: "%@ — включено",
    uk: "%@ — увімкнено", zh: "%@ — 已开启", ar: "%@ — مُفعَّل", fa: "%@ — فعال"
  },
  provisioning_method_off: {
    en: "%@ — off", es: "%@ — desactivado", fr: "%@ — désactivée", ru: "%@ — выключено",
    uk: "%@ — вимкнено", zh: "%@ — 已关闭", ar: "%@ — مُعطَّل", fa: "%@ — غیرفعال"
  },
  provisioning_deaddrop_description: {
    en: "If the methods above run out of addresses, the app can fetch replacements from a signed public file.",
    es: "Si los métodos anteriores se quedan sin direcciones, la app puede buscar reemplazos en un archivo público firmado.",
    fr: "Si les méthodes ci-dessus n'ont plus d'adresses, l'app peut en chercher dans un fichier public signé.",
    ru: "Если у методов выше не осталось адресов, приложение может взять новые из подписанного общедоступного файла.",
    uk: "Якщо в методів вище не лишилося адрес, застосунок може взяти нові з підписаного загальнодоступного файлу.",
    zh: "若上述方式都没有可用地址，应用可从带签名的公开文件获取替代地址。",
    ar: "إذا نفدت عناوين الطرق أعلاه، يمكن للتطبيق جلب بدائل من ملف عام موقَّع.",
    fa: "اگر روش‌های بالا نشانی نداشته باشند، برنامه می‌تواند جایگزین‌ها را از یک فایل عمومی امضاشده بگیرد."
  },
  provisioning_deaddrop_on: {
    en: "Address recovery on — used only when every method above has failed.",
    es: "Recuperación de direcciones activada: solo se usa cuando todos los métodos anteriores fallan.",
    fr: "Récupération d'adresses activée — utilisée uniquement si toutes les méthodes ci-dessus échouent.",
    ru: "Восстановление адресов включено — используется, только если все методы выше не сработали.",
    uk: "Відновлення адрес увімкнено — використовується, лише якщо всі методи вище не спрацювали.",
    zh: "地址恢复已开启——仅在上述所有方式都失败时使用。",
    ar: "استعادة العناوين مفعّلة — تُستخدم فقط عند فشل كل الطرق أعلاه.",
    fa: "بازیابی نشانی‌ها روشن است — تنها زمانی استفاده می‌شود که همه روش‌های بالا شکست بخورند."
  },
  provisioning_deaddrop_off: {
    en: "Address recovery off.",
    es: "Recuperación de direcciones desactivada.",
    fr: "Récupération d'adresses désactivée.",
    ru: "Восстановление адресов выключено.",
    uk: "Відновлення адрес вимкнено.",
    zh: "地址恢复已关闭。",
    ar: "استعادة العناوين متوقفة.",
    fa: "بازیابی نشانی‌ها خاموش است."
  },
  provisioning_normal_test_title: {
    en: "Test the normal connection?",
    es: "¿Probar la conexión normal?",
    fr: "Tester la connexion normale ?",
    ru: "Проверить обычное подключение?",
    uk: "Перевірити звичайне з'єднання?",
    zh: "测试常规连接？",
    ar: "هل تريد اختبار الاتصال العادي؟",
    fa: "اتصال عادی آزمایش شود؟"
  },
  provisioning_normal_test_message: {
    en: "It's off because it shows our domain to anyone watching your network. Testing it does the same, once.",
    es: "Está desactivada porque muestra nuestro dominio a cualquiera que vigile tu red. Probarla hace lo mismo, una vez.",
    fr: "Elle est désactivée, car elle montre notre domaine à quiconque surveille votre réseau. La tester fait de même, une fois.",
    ru: "Оно выключено, потому что показывает наш домен всем, кто следит за вашей сетью. Проверка сделает то же самое один раз.",
    uk: "Його вимкнено, бо воно показує наш домен усім, хто стежить за вашою мережею. Перевірка зробить те саме один раз.",
    zh: "它处于关闭状态，因为它会向监视你网络的人暴露我们的域名。测试也会这样做一次。",
    ar: "إنه مُعطَّل لأنه يكشف نطاقنا لأي شخص يراقب شبكتك. واختباره يفعل الشيء نفسه مرة واحدة.",
    fa: "خاموش است چون دامنه ما را به هر کسی که شبکه شما را زیر نظر دارد نشان می‌دهد. آزمایش آن هم یک بار همین کار را می‌کند."
  },
  provisioning_normal_test_confirm: {
    en: "Test anyway", es: "Probar de todos modos", fr: "Tester quand même", ru: "Всё равно проверить",
    uk: "Однаково перевірити", zh: "仍然测试", ar: "اختبار على أي حال", fa: "به‌هرحال آزمایش کن"
  },
  provisioning_normal_test_cancel: {
    en: "Cancel", es: "Cancelar", fr: "Annuler", ru: "Отмена",
    uk: "Скасувати", zh: "取消", ar: "إلغاء", fa: "لغو"
  },
  network_pq_title: {
    en: "Post-quantum key", es: "Clave poscuántica", fr: "Clé post-quantique", ru: "Постквантовый ключ",
    uk: "Постквантовий ключ", zh: "后量子密钥", ar: "مفتاح ما بعد الكم", fa: "کلید پساکوانتومی"
  },
  network_pq_hint: {
    en: "Adds a quantum-resistant key to the tunnel, so traffic recorded today can't be decrypted later. Applies on next connect.",
    es: "Añade al túnel una clave resistente a la computación cuántica: el tráfico grabado hoy no podrá descifrarse después. Se aplica en la próxima conexión.",
    fr: "Ajoute au tunnel une clé résistante au quantique : le trafic enregistré aujourd'hui ne pourra pas être déchiffré plus tard. Effectif à la prochaine connexion.",
    ru: "Добавляет в туннель ключ, устойчивый к квантовым вычислениям: перехваченный сегодня трафик не удастся расшифровать позже. Действует со следующего подключения.",
    uk: "Додає в тунель ключ, стійкий до квантових обчислень: перехоплений сьогодні трафік не вдасться розшифрувати згодом. Діє з наступного підключення.",
    zh: "为隧道添加抗量子密钥，今天被截获的流量日后也无法解密。下次连接时生效。",
    ar: "يضيف إلى النفق مفتاحًا مقاومًا للحوسبة الكمية، فلا يمكن فك تشفير الحركة الملتقطة اليوم لاحقًا. يسري عند الاتصال التالي.",
    fa: "کلیدی مقاوم در برابر رایانش کوانتومی به تونل اضافه می‌کند تا ترافیک ضبط‌شده امروز بعداً رمزگشایی نشود. از اتصال بعدی اعمال می‌شود."
  },
  network_pq_on: {
    en: "Post-quantum key enabled. Reconnect for it to take effect.",
    es: "Clave poscuántica activada. Vuelve a conectar para que surta efecto.",
    fr: "Clé post-quantique activée. Reconnectez-vous pour qu’elle prenne effet.",
    ru: "«Постквантовый ключ» включён. Переподключитесь, чтобы изменение вступило в силу.",
    uk: "Постквантовий ключ увімкнено. Перепідключіться, щоб зміни набули чинності.",
    zh: "已启用后量子密钥。请重新连接以生效。",
    ar: "تم تفعيل مفتاح ما بعد الكم. أعد الاتصال ليسري المفعول.",
    fa: "کلید پساکوانتومی فعال شد. برای اعمال، دوباره متصل شوید."
  },
  network_pq_off: {
    en: "Post-quantum key disabled. Reconnect for it to take effect.",
    es: "Clave poscuántica desactivada. Vuelve a conectar para que surta efecto.",
    fr: "Clé post-quantique désactivée. Reconnectez-vous pour qu’elle prenne effet.",
    ru: "«Постквантовый ключ» выключен. Переподключитесь, чтобы изменение вступило в силу.",
    uk: "Постквантовий ключ вимкнено. Перепідключіться, щоб зміни набули чинності.",
    zh: "已禁用后量子密钥。请重新连接以生效。",
    ar: "تم تعطيل مفتاح ما بعد الكم. أعد الاتصال ليسري المفعول.",
    fa: "کلید پساکوانتومی غیرفعال شد. برای اعمال، دوباره متصل شوید."
  },
  network_allowlan_on: {
    en: "Allow LAN enabled. Reconnect for it to take effect.",
    es: "Permitir LAN activado. Vuelve a conectar para que surta efecto.",
    fr: "Autorisation du LAN activée. Reconnectez-vous pour qu'elle prenne effet.",
    ru: "«Разрешить LAN» включено. Переподключитесь, чтобы изменение вступило в силу.",
    uk: "Дозвіл LAN увімкнено. Перепідключіться, щоб зміни набули чинності.",
    zh: "已启用允许 LAN。请重新连接以生效。",
    ar: "تم تفعيل السماح بشبكة LAN. أعد الاتصال ليسري المفعول.",
    fa: "اجازه به LAN فعال شد. برای اعمال، دوباره متصل شوید."
  },
  network_allowlan_off: {
    en: "Allow LAN disabled. Reconnect for it to take effect.",
    es: "Permitir LAN desactivado. Vuelve a conectar para que surta efecto.",
    fr: "Autorisation du LAN désactivée. Reconnectez-vous pour qu'elle prenne effet.",
    ru: "«Разрешить LAN» выключено. Переподключитесь, чтобы изменение вступило в силу.",
    uk: "Дозвіл LAN вимкнено. Перепідключіться, щоб зміни набули чинності.",
    zh: "已禁用允许 LAN。请重新连接以生效。",
    ar: "تم تعطيل السماح بشبكة LAN. أعد الاتصال ليسري المفعول.",
    fa: "اجازه به LAN غیرفعال شد. برای اعمال، دوباره متصل شوید."
  },
  network_dns_provider_title: {
    en: "DNS Provider", es: "Proveedor DNS", fr: "Fournisseur DNS", ru: "DNS-провайдер",
    uk: "DNS-провайдер", zh: "DNS 提供商", ar: "مزود DNS", fa: "ارائه‌دهنده DNS"
  },
  network_dns_provider_hint: {
    en: "Pick a resolver, or enter your own. Filtering comes from that service. Applies on next connect.",
    es: "Elige un servicio DNS, o introduce el tuyo. El filtrado viene de ese servicio. Se aplica en la próxima conexión.",
    fr: "Choisissez un service DNS, ou saisissez le vôtre. Le filtrage vient de ce service. Effectif à la prochaine connexion.",
    ru: "Выберите DNS-сервис или укажите свой. Фильтрация идёт от него. Действует со следующего подключения.",
    uk: "Виберіть DNS-сервіс або введіть власний. Фільтрація йде від нього. Діє з наступного підключення.",
    zh: "选一个 DNS 服务或填自己的。过滤来自该服务。下次连接时生效。",
    ar: "اختر خدمة DNS أو أدخل عناوينك. التصفية تأتي من تلك الخدمة. يسري عند الاتصال التالي.",
    fa: "یک سرویس DNS انتخاب کنید یا نشانی خودتان را وارد کنید. فیلترسازی از همان سرویس می‌آید. از اتصال بعدی اعمال می‌شود."
  },
  network_dns_automatic: {
    en: "Automatic (VPN default)",
    es: "Automático (predeterminado de la VPN)",
    fr: "Automatique (valeur VPN par défaut)",
    ru: "Автоматически (DNS VPN по умолчанию)",
    uk: "Автоматично (типовий DNS VPN)",
    zh: "自动（VPN 默认值）",
    ar: "تلقائي (إعداد VPN الافتراضي)",
    fa: "خودکار (پیش‌فرض VPN)"
  },
  network_dns_adguard: {
    en: "AdGuard DNS - Ads & trackers",
    es: "AdGuard DNS - Anuncios y rastreadores",
    fr: "AdGuard DNS - Publicités et traqueurs",
    ru: "AdGuard DNS - Реклама и трекеры",
    uk: "AdGuard DNS - Реклама й трекери",
    zh: "AdGuard DNS - 屏蔽广告和跟踪器",
    ar: "AdGuard DNS - حظر الإعلانات والمتتبعات",
    fa: "AdGuard DNS - مسدودسازی تبلیغات و ردیاب‌ها"
  },
  network_dns_cloudflare: {
    en: "Cloudflare", es: "Cloudflare", fr: "Cloudflare", ru: "Cloudflare",
    uk: "Cloudflare", zh: "Cloudflare", ar: "Cloudflare", fa: "Cloudflare"
  },
  network_dns_quad9: {
    en: "Quad9 - Malware blocking",
    es: "Quad9 - Bloqueo de malware",
    fr: "Quad9 - Blocage des logiciels malveillants",
    ru: "Quad9 - Блокировка вредоносных сайтов",
    uk: "Quad9 - Блокування шкідливих сайтів",
    zh: "Quad9 - 屏蔽恶意网站",
    ar: "Quad9 - حظر المواقع الضارة",
    fa: "Quad9 - مسدودسازی سایت‌های مخرب"
  },
  network_dns_cloudflare_family: {
    en: "Cloudflare Family - Malware & adult content",
    es: "Cloudflare Family - Malware y contenido para adultos",
    fr: "Cloudflare Family - Logiciels malveillants et contenu adulte",
    ru: "Cloudflare Family - Вредоносные сайты и контент для взрослых",
    uk: "Cloudflare Family - Шкідливі сайти й контент для дорослих",
    zh: "Cloudflare Family - 屏蔽恶意和成人内容",
    ar: "Cloudflare Family - حظر المحتوى الضار ومحتوى البالغين",
    fa: "Cloudflare Family - محتوای مخرب و بزرگسالان"
  },
  network_dns_google: {
    en: "Google Public DNS", es: "DNS público de Google", fr: "DNS public de Google", ru: "Публичный DNS Google",
    uk: "Публічний DNS Google", zh: "Google 公共 DNS", ar: "DNS العام من Google", fa: "DNS عمومی Google"
  },
  network_dns_custom: {
    en: "Custom", es: "Personalizado", fr: "Personnalisé", ru: "Пользовательский",
    uk: "Власний", zh: "自定义", ar: "مخصص", fa: "سفارشی"
  },
  network_dns_saved: {
    en: "DNS set to %@. Takes effect on next connect.",
    es: "DNS establecido en %@. Se aplica en la próxima conexión.",
    fr: "DNS défini sur %@. Prend effet à la prochaine connexion.",
    ru: "DNS установлен на %@. Вступает в силу при следующем подключении.",
    uk: "DNS встановлено на %@. Набуває чинності під час наступного підключення.",
    zh: "DNS 已设置为 %@。将在下次连接时生效。",
    ar: "تم تعيين DNS إلى %@. يسري عند الاتصال التالي.",
    fa: "DNS روی %@ تنظیم شد. از اتصال بعدی اعمال می‌شود."
  },
  network_dns_default_saved: {
    en: "DNS restored to the VPN default. Takes effect on next connect.",
    es: "DNS restaurado al valor predeterminado de la VPN. Se aplica en la próxima conexión.",
    fr: "DNS restauré à la valeur VPN par défaut. Prend effet à la prochaine connexion.",
    ru: "Восстановлен DNS VPN по умолчанию. Вступает в силу при следующем подключении.",
    uk: "DNS відновлено до типового значення VPN. Набуває чинності під час наступного підключення.",
    zh: "DNS 已恢复为 VPN 默认值。将在下次连接时生效。",
    ar: "تمت استعادة DNS إلى إعداد VPN الافتراضي. يسري عند الاتصال التالي.",
    fa: "DNS به پیش‌فرض VPN بازگردانده شد. از اتصال بعدی اعمال می‌شود."
  },
  network_mtu_invalid: {
    en: "Enter a whole number between %lld and %lld.",
    es: "Introduce un número entero entre %lld y %lld.",
    fr: "Saisissez un nombre entier entre %lld et %lld.",
    ru: "Введите целое число от %lld до %lld.",
    uk: "Введіть ціле число від %lld до %lld.",
    zh: "请输入 %lld 到 %lld 之间的整数。",
    ar: "أدخل عددًا صحيحًا بين %lld و %lld.",
    fa: "یک عدد صحیح بین %lld و %lld وارد کنید."
  },
  network_mtu_saved: {
    en: "Tunnel MTU set to %lld. Takes effect on next connect.",
    es: "MTU del túnel establecida en %lld. Se aplica en la próxima conexión.",
    fr: "MTU du tunnel définie sur %lld. Prend effet à la prochaine connexion.",
    ru: "MTU туннеля установлен на %lld. Вступает в силу при следующем подключении.",
    uk: "MTU тунелю встановлено на %lld. Набуває чинності під час наступного підключення.",
    zh: "隧道 MTU 已设置为 %lld。将在下次连接时生效。",
    ar: "تم ضبط MTU النفق على %lld. يسري عند الاتصال التالي.",
    fa: "MTU تونل روی %lld تنظیم شد. از اتصال بعدی اعمال می‌شود."
  },
  network_summary_mtu: {
    en: "MTU %lld", es: "MTU %lld", fr: "MTU %lld", ru: "MTU %lld",
    uk: "MTU %lld", zh: "MTU %lld", ar: "MTU %lld", fa: "MTU %lld"
  },
  network_summary_pq: {
    en: "PQ", es: "PQ", fr: "PQ", ru: "PQ",
    uk: "PQ", zh: "PQ", ar: "PQ", fa: "PQ"
  }
};
