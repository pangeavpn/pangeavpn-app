// Sign-in, device limit, expired panel and app root. Wording follows desktop, minus any
// billing or purchase prompt (App Store rule 3.1.1).
export default {
  login_find_account: {
    en: "Find your account number", es: "Encuentra tu número de cuenta", fr: "Trouver votre numéro de compte",
    ru: "Найти номер аккаунта", uk: "Знайти номер акаунта", zh: "查找您的账号",
    ar: "اعثر على رقم حسابك", fa: "شماره حساب خود را پیدا کنید"
  },
  login_error_invalid_account: {
    en: "We don't recognise that account number. Check it and try again.",
    es: "No reconocemos ese número de cuenta. Compruébalo e inténtalo de nuevo.",
    fr: "Ce numéro de compte n'est pas reconnu. Vérifiez-le et réessayez.",
    ru: "Такой номер счёта не найден. Проверьте его и попробуйте снова.",
    uk: "Такий номер акаунта не знайдено. Перевірте його та спробуйте ще раз.",
    zh: "无法识别该账号。请检查后重试。",
    ar: "رقم الحساب هذا غير معروف. تحقق منه وحاول مرة أخرى.",
    fa: "این شماره حساب شناخته نشد. آن را بررسی کنید و دوباره تلاش کنید."
  },
  login_error_expired: {
    en: "This account has no time left. Once time is added to your account, sign in again.",
    es: "Esta cuenta no tiene tiempo restante. Cuando se añada tiempo a tu cuenta, vuelve a iniciar sesión.",
    fr: "Ce compte n'a plus de temps. Une fois du temps ajouté à votre compte, reconnectez-vous.",
    ru: "На этом аккаунте не осталось времени. Когда на аккаунт будет добавлено время, войдите снова.",
    uk: "На цьому акаунті не залишилося часу. Коли на акаунт буде додано час, увійдіть знову.",
    zh: "此账户已无剩余时长。账户添加时长后，请重新登录。",
    ar: "لم يتبقَّ وقت في هذا الحساب. بعد إضافة وقت إلى حسابك، سجّل الدخول مجددًا.",
    fa: "این حساب زمانی باقی‌مانده ندارد. پس از افزودن زمان به حسابتان، دوباره وارد شوید."
  },
  login_error_device_limit: {
    en: "This account already has the maximum number of devices. Remove one to continue.",
    es: "Esta cuenta ya tiene el máximo de dispositivos. Elimina uno para continuar.",
    fr: "Ce compte a déjà atteint le nombre maximal d'appareils. Supprimez-en un pour continuer.",
    ru: "На этом аккаунте уже максимальное число устройств. Удалите одно, чтобы продолжить.",
    uk: "Цей акаунт уже має максимальну кількість пристроїв. Видаліть один, щоб продовжити.",
    zh: "此账号的设备数量已达上限。请移除一台后继续。",
    ar: "بلغ هذا الحساب الحد الأقصى لعدد الأجهزة. أزل جهازًا للمتابعة.",
    fa: "این حساب به حداکثر تعداد دستگاه رسیده است. یکی را حذف کنید تا ادامه دهید."
  },
  login_error_rate_limited: {
    en: "Too many sign-in attempts. Wait a minute, then try again.",
    es: "Demasiados intentos de inicio de sesión. Espera un minuto e inténtalo de nuevo.",
    fr: "Trop de tentatives de connexion. Attendez une minute, puis réessayez.",
    ru: "Слишком много попыток входа. Подождите минуту и попробуйте снова.",
    uk: "Забагато спроб входу. Зачекайте хвилину та спробуйте ще раз.",
    zh: "登录尝试过于频繁。请稍等一分钟后重试。",
    ar: "محاولات تسجيل دخول كثيرة. انتظر دقيقة ثم حاول مرة أخرى.",
    fa: "تلاش‌های ورود بیش از حد زیاد بود. یک دقیقه صبر کنید و دوباره تلاش کنید."
  },
  login_error_server: {
    en: "Our servers are having trouble. Please try again in a moment.",
    es: "Nuestros servidores tienen problemas. Inténtalo de nuevo en un momento.",
    fr: "Nos serveurs rencontrent un problème. Réessayez dans un instant.",
    ru: "На наших серверах неполадки. Повторите попытку через минуту.",
    uk: "На наших серверах неполадки. Спробуйте ще раз за мить.",
    zh: "我们的服务器出现问题。请稍后重试。",
    ar: "تواجه خوادمنا مشكلة. حاول مرة أخرى بعد قليل.",
    fa: "سرورهای ما با مشکل مواجه شده‌اند. کمی بعد دوباره تلاش کنید."
  },
  login_error_unreachable: {
    en: "Couldn't reach PangeaVPN. Check your internet connection and try again.",
    es: "No se pudo conectar con PangeaVPN. Comprueba tu conexión a internet e inténtalo de nuevo.",
    fr: "Impossible de joindre PangeaVPN. Vérifiez votre connexion internet et réessayez.",
    ru: "Не удалось связаться с PangeaVPN. Проверьте интернет-соединение и попробуйте снова.",
    uk: "Не вдалося зв'язатися з PangeaVPN. Перевірте інтернет-з'єднання та спробуйте ще раз.",
    zh: "无法连接到 PangeaVPN。请检查网络连接后重试。",
    ar: "تعذّر الوصول إلى PangeaVPN. تحقق من اتصالك بالإنترنت وحاول مرة أخرى.",
    fa: "دسترسی به PangeaVPN ممکن نشد. اتصال اینترنت خود را بررسی کنید و دوباره تلاش کنید."
  },
  login_error_timeout: {
    en: "Signing in took too long. Check your connection and try again.",
    es: "El inicio de sesión tardó demasiado. Comprueba tu conexión e inténtalo de nuevo.",
    fr: "La connexion a pris trop de temps. Vérifiez votre connexion et réessayez.",
    ru: "Вход занял слишком много времени. Проверьте соединение и попробуйте снова.",
    uk: "Вхід тривав задовго. Перевірте з'єднання та спробуйте ще раз.",
    zh: "登录超时。请检查网络连接后重试。",
    ar: "استغرق تسجيل الدخول وقتًا طويلاً. تحقق من اتصالك وحاول مرة أخرى.",
    fa: "ورود بیش از حد طول کشید. اتصال خود را بررسی کنید و دوباره تلاش کنید."
  },
  login_error_registration: {
    en: "Your account is fine, but this device couldn't be registered. Please try again.",
    es: "Tu cuenta está bien, pero no se pudo registrar este dispositivo. Inténtalo de nuevo.",
    fr: "Votre compte est valide, mais cet appareil n'a pas pu être enregistré. Réessayez.",
    ru: "С аккаунтом всё в порядке, но зарегистрировать это устройство не удалось. Попробуйте снова.",
    uk: "З акаунтом усе гаразд, але зареєструвати цей пристрій не вдалося. Спробуйте ще раз.",
    zh: "账号正常，但此设备注册失败。请重试。",
    ar: "حسابك سليم، لكن تعذّر تسجيل هذا الجهاز. حاول مرة أخرى.",
    fa: "حساب شما مشکلی ندارد، اما ثبت این دستگاه ممکن نشد. دوباره تلاش کنید."
  },
  login_error_unknown: {
    en: "Sign in failed. Please try again.", es: "Error al iniciar sesión. Inténtalo de nuevo.",
    fr: "Échec de la connexion. Veuillez réessayer.", ru: "Не удалось войти. Попробуйте снова.",
    uk: "Не вдалося увійти. Спробуйте ще раз.", zh: "登录失败。请重试。",
    ar: "فشل تسجيل الدخول. حاول مرة أخرى.", fa: "ورود ناموفق بود. دوباره تلاش کنید."
  },
  devicelimit_continue: {
    en: "Continue Sign In", es: "Continuar inicio de sesión", fr: "Poursuivre la connexion",
    ru: "Продолжить вход", uk: "Продовжити вхід", zh: "继续登录",
    ar: "متابعة تسجيل الدخول", fa: "ادامه ورود"
  },
  devicelimit_cancel: {
    en: "Cancel & Sign Out", es: "Cancelar y cerrar sesión", fr: "Annuler et se déconnecter",
    ru: "Отменить и выйти", uk: "Скасувати та вийти", zh: "取消并退出登录",
    ar: "إلغاء وتسجيل الخروج", fa: "لغو و خروج"
  },
  devicelimit_load_failed: {
    en: "Could not load devices. Please try again.",
    es: "No se pudieron cargar los dispositivos. Inténtalo de nuevo.",
    fr: "Impossible de charger les appareils. Veuillez réessayer.",
    ru: "Не удалось загрузить устройства. Попробуйте ещё раз.",
    uk: "Не вдалося завантажити пристрої. Спробуйте ще раз.",
    zh: "无法加载设备，请重试。",
    ar: "تعذّر تحميل الأجهزة. يرجى المحاولة مرة أخرى.",
    fa: "بارگذاری دستگاه‌ها ممکن نشد. لطفاً دوباره تلاش کنید."
  },
  devicelimit_none_left: {
    en: "No devices found. You can continue signing in now.",
    es: "No se encontraron dispositivos. Ya puedes continuar con el inicio de sesión.",
    fr: "Aucun appareil trouvé. Vous pouvez poursuivre la connexion maintenant.",
    ru: "Устройства не найдены. Теперь можно продолжить вход.",
    uk: "Пристроїв не знайдено. Тепер можете продовжити вхід.",
    zh: "未找到设备。您现在可以继续登录。",
    ar: "لم يتم العثور على أجهزة. يمكنك متابعة تسجيل الدخول الآن.",
    fa: "دستگاهی یافت نشد. اکنون می‌توانید ورود را ادامه دهید."
  },
  devicelimit_still_at_limit: {
    en: "Still at device limit. Please remove another device.",
    es: "Aún estás en el límite de dispositivos. Elimina otro dispositivo.",
    fr: "Limite d'appareils toujours atteinte. Veuillez supprimer un autre appareil.",
    ru: "Лимит устройств всё ещё превышен. Удалите ещё одно устройство.",
    uk: "Ліміт пристроїв досі перевищено. Видаліть ще один пристрій.",
    zh: "仍处于设备上限。请再移除一台设备。",
    ar: "ما زلت عند حد الأجهزة. يرجى إزالة جهاز آخر.",
    fa: "هنوز در سقف تعداد دستگاه‌ها هستید. لطفاً دستگاه دیگری را حذف کنید."
  },
  devicelimit_this_device: {
    en: "This device", es: "Este dispositivo", fr: "Cet appareil", ru: "Это устройство",
    uk: "Цей пристрій", zh: "此设备", ar: "هذا الجهاز", fa: "این دستگاه"
  },
  devicelimit_added: {
    en: "Added %@", es: "Añadido el %@", fr: "Ajouté le %@", ru: "Добавлено %@",
    uk: "Додано %@", zh: "添加于 %@", ar: "أُضيف في %@", fa: "افزوده‌شده در %@"
  },
  devicelimit_unnamed: {
    en: "Unnamed device", es: "Dispositivo sin nombre", fr: "Appareil sans nom", ru: "Устройство без имени",
    uk: "Пристрій без назви", zh: "未命名设备", ar: "جهاز بدون اسم", fa: "دستگاه بی‌نام"
  },
  devicelimit_removing: {
    en: "Removing...", es: "Eliminando...", fr: "Suppression...", ru: "Удаление...",
    uk: "Видалення...", zh: "正在移除...", ar: "جارٍ الإزالة...", fa: "در حال حذف..."
  },
  devicelimit_removed: {
    en: "Device removed.", es: "Dispositivo eliminado.", fr: "Appareil supprimé.", ru: "Устройство удалено.",
    uk: "Пристрій видалено.", zh: "设备已移除。", ar: "تمت إزالة الجهاز.", fa: "دستگاه حذف شد."
  },
  devicelimit_remove_failed: {
    en: "Failed to remove device. Please try again.",
    es: "No se pudo eliminar el dispositivo. Inténtalo de nuevo.",
    fr: "Échec de la suppression de l'appareil. Veuillez réessayer.",
    ru: "Не удалось удалить устройство. Попробуйте ещё раз.",
    uk: "Не вдалося видалити пристрій. Спробуйте ще раз.",
    zh: "移除设备失败，请重试。",
    ar: "فشل إزالة الجهاز. يرجى المحاولة مرة أخرى.",
    fa: "حذف دستگاه ناموفق بود. لطفاً دوباره تلاش کنید."
  },
  expired_title: {
    en: "Your time has run out", es: "Se te ha acabado el tiempo", fr: "Votre temps est écoulé",
    ru: "Ваше время истекло", uk: "Ваш час вичерпано", zh: "您的时长已用完",
    ar: "انتهى وقتك", fa: "زمان شما تمام شده است"
  },
  expired_body: {
    en: "This account has no time left, so PangeaVPN can't connect. Once time is added to your account, check again.",
    es: "Esta cuenta no tiene tiempo restante, así que PangeaVPN no puede conectarse. Cuando se añada tiempo a tu cuenta, vuelve a comprobarlo.",
    fr: "Ce compte n'a plus de temps, donc PangeaVPN ne peut pas se connecter. Une fois du temps ajouté à votre compte, vérifiez à nouveau.",
    ru: "На этом аккаунте не осталось времени, поэтому PangeaVPN не может подключиться. Когда на аккаунт будет добавлено время, проверьте снова.",
    uk: "На цьому акаунті не залишилося часу, тому PangeaVPN не може підключитися. Коли на акаунт буде додано час, перевірте знову.",
    zh: "此账户已无剩余时长，PangeaVPN 无法连接。账户添加时长后，请重新检查。",
    ar: "لم يتبقَّ وقت في هذا الحساب، لذا لا يمكن لـ PangeaVPN الاتصال. بعد إضافة وقت إلى حسابك، تحقق مجددًا.",
    fa: "این حساب زمانی باقی‌مانده ندارد، بنابراین PangeaVPN نمی‌تواند وصل شود. پس از افزودن زمان به حسابتان، دوباره بررسی کنید."
  },
  expired_ran_out: {
    en: "Ran out on %@", es: "Se agotó el %@", fr: "Épuisé le %@", ru: "Закончилось %@",
    uk: "Закінчився %@", zh: "已于 %@ 用完", ar: "انتهى في %@", fa: "در %@ تمام شد"
  },
  expired_check_again: {
    en: "Check again", es: "Comprobar de nuevo", fr: "Vérifier à nouveau", ru: "Проверить снова",
    uk: "Перевірити знову", zh: "重新检查", ar: "تحقق مجددًا", fa: "بررسی دوباره"
  },
  expired_sign_out: {
    en: "Sign Out", es: "Cerrar sesión", fr: "Se déconnecter", ru: "Выйти",
    uk: "Вийти", zh: "退出登录", ar: "تسجيل الخروج", fa: "خروج"
  },
  expired_restored: {
    en: "Time added — you can connect again.",
    es: "Tiempo añadido: ya puedes conectarte de nuevo.",
    fr: "Temps ajouté : vous pouvez de nouveau vous connecter.",
    ru: "Время добавлено — можно снова подключаться.",
    uk: "Час додано — можна знову підключатися.",
    zh: "时长已添加，您可以重新连接了。",
    ar: "تمت إضافة الوقت — يمكنك الاتصال مجددًا.",
    fa: "زمان اضافه شد — می‌توانید دوباره وصل شوید."
  },
  expired_still: {
    en: "Still no active subscription.", es: "Sigue sin haber una suscripción activa.",
    fr: "Toujours aucun abonnement actif.", ru: "Активной подписки по-прежнему нет.",
    uk: "Активної підписки досі немає.", zh: "仍无有效订阅。",
    ar: "لا يوجد اشتراك نشط حتى الآن.", fa: "هنوز اشتراک فعالی وجود ندارد."
  },
  expired_check_failed: {
    en: "Couldn't check right now.", es: "No se pudo comprobar ahora.",
    fr: "Vérification impossible pour le moment.", ru: "Сейчас не удалось проверить.",
    uk: "Зараз не вдалося перевірити.", zh: "暂时无法检查。",
    ar: "تعذّر التحقق الآن.", fa: "در حال حاضر بررسی ممکن نشد."
  },
  root_data_erased: {
    en: "PangeaVPN data erased.", es: "Datos de PangeaVPN borrados.", fr: "Données de PangeaVPN effacées.",
    ru: "Данные PangeaVPN удалены.", uk: "Дані PangeaVPN видалено.", zh: "PangeaVPN 数据已清除。",
    ar: "تم مسح بيانات PangeaVPN.", fa: "داده‌های PangeaVPN پاک شد."
  }
};
