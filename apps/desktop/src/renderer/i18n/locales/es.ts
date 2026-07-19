// Spanish (es) — machine translation, pending native review.
import type { Messages } from "../messages";

export const es = {
  // ── App shell / loading ───────────────────────────────
  "app.brand": "Pangea VPN",
  "app.loading.starting": "Iniciando PangeaVPN...",
  "app.loading.progress": "Preparando todo... ({remaining}s)",
  "app.loading.cantStart": "PangeaVPN no pudo iniciarse. Reinicia la aplicación.",
  "app.loading.didntStart": "PangeaVPN no se inició. Reinicia la aplicación.",

  // ── Login screen ──────────────────────────────────────
  "login.subtitle": "Introduce tu token de acceso para continuar.",
  "login.getToken": "Obtén tu token",
  "login.tokenPlaceholder": "0000000000000000",
  "login.tokenAriaLabel": "Token de acceso",
  "login.signIn": "Iniciar sesión",
  "login.cachedTokenAriaLabel": "Iniciar sesión con el token guardado",
  "login.enterToken": "Introduce tu token de VPN.",
  "login.signingIn": "Iniciando sesión...",
  "login.invalidToken": "Token de VPN no válido.",
  "login.signInFailed": "Error al iniciar sesión.",

  // ── Device-limit screen ───────────────────────────────
  "deviceLimit.title": "Límite de dispositivos alcanzado",
  "deviceLimit.subtitle": "Tu cuenta alcanzó el número máximo de dispositivos. Elimina uno para seguir con el inicio de sesión.",
  "deviceLimit.continue": "Continuar inicio de sesión",
  "deviceLimit.cancel": "Cancelar y cerrar sesión",
  "deviceLimit.loadFailed": "No se pudieron cargar los dispositivos. Inténtalo de nuevo.",
  "deviceLimit.noneCanContinue": "No se encontraron dispositivos. Ya puedes continuar con el inicio de sesión.",
  "deviceLimit.stillAtLimit": "Aún estás en el límite de dispositivos. Elimina otro dispositivo.",

  // ── Devices (modal + shared rows) ─────────────────────
  "devices.title": "Dispositivos",
  "devices.subtitle": "Dispositivos con sesión iniciada en tu cuenta.",
  "devices.thisDevice": "Este dispositivo",
  "devices.added": "Añadido el {date}",
  "devices.current": "Actual",
  "devices.currentTitle": "Cierra sesión para eliminar este dispositivo",
  "devices.remove": "Eliminar",
  "devices.removing": "Eliminando...",
  "devices.removed": "Dispositivo eliminado.",
  "devices.removeFailed": "No se pudo eliminar el dispositivo. Inténtalo de nuevo.",
  "devices.none": "No se encontraron dispositivos.",
  "devices.noneRemaining": "No quedan dispositivos.",

  // ── Header / menu ─────────────────────────────────────
  "header.brandAriaLabel": "Pangea VPN",
  "header.signIn": "Iniciar sesión",
  "header.toggleTheme": "Cambiar tema",
  "header.menu": "Menú",
  "menu.settings": "Ajustes",
  "menu.updateAvailable": "Actualización disponible",

  // ── Hero connection card ──────────────────────────────
  "hero.selectServer": "Seleccionar servidor",
  "hero.refreshServers": "Actualizar servidores",
  "hero.connect": "Conectar",
  "hero.disconnect": "Desconectar",
  "hero.provisioning": "Aprovisionando...",
  "hero.disconnecting": "Desconectando...",
  "hero.noServers": "No hay servidores disponibles",

  // ── Status pills ──────────────────────────────────────
  "pill.killSwitch": "Kill Switch",
  "pill.cloak": "Camuflaje",
  "pill.wireguard": "WireGuard",

  // ── Connection state labels (hero + tray) ─────────────
  "state.DISCONNECTED": "DESCONECTADO",
  "state.CONNECTING": "CONECTANDO",
  "state.CONNECTED": "CONECTADO",
  "state.DISCONNECTING": "DESCONECTANDO",
  "state.ERROR": "ERROR",

  // ── Logs panel ────────────────────────────────────────
  "logs.title": "Registros",
  "logs.copyDiagnostics": "Copiar diagnóstico",
  "logs.copyLogs": "Copiar registros",
  "logs.clear": "Borrar",
  "logs.noneToCopy": "No hay registros para copiar.",
  "logs.copied": "Registros copiados al portapapeles.",
  "logs.cleared": "Registros borrados.",
  "logs.diagnosticsCopied": "Diagnóstico copiado al portapapeles.",
  "logs.bridgeUnavailable": "Puente daemonApi no disponible.",

  // ── Settings overlay ──────────────────────────────────
  "settings.title": "Ajustes",
  "settings.close": "Cerrar",
  "settings.account.heading": "Cuenta",
  "settings.account.signedInAs": "Sesión iniciada como",
  "settings.account.subscription": "Suscripción",
  "settings.account.token": "Token de acceso",
  "settings.account.show": "Mostrar",
  "settings.account.hide": "Ocultar",
  "settings.account.copy": "Copiar",
  "settings.account.tokenHint": "Este es el token con el que iniciaste sesión. Mantenlo en privado — cualquiera que lo tenga puede acceder a tu cuenta.",
  "settings.account.manageSub": "Gestionar suscripción",
  "settings.account.devices": "Dispositivos",
  "settings.account.signOut": "Cerrar sesión",
  "settings.censorship.heading": "Elusión de censura",
  "settings.censorship.description": "Actívalo si tu red bloquea el acceso a los servicios de VPN.",
  "settings.censorship.directIp.title": "IP directa",
  "settings.censorship.directIp.hint": "Conéctate a nuestros servidores por dirección IP, omitiendo DNS por completo.",
  "settings.censorship.directIpOnly.title": "Solo IP directa",
  "settings.censorship.directIpOnly.hint": "Usa siempre conexiones por IP directa. Omite por completo las llamadas normales a la API — úsalo si tu red bloquea HTTPS hacia nuestros servidores.",
  "settings.transport.heading": "Connection Method",
  "settings.transport.description": "How PangeaVPN disguises your traffic.",
  "settings.transport.auto": "Automatic (recommended)",
  "settings.transport.cloak": "Cloak only",
  "settings.transport.naive": "NaiveProxy only",
  "settings.transport.hysteria2": "Hysteria2 only",
  "settings.network.heading": "Red",
  "settings.network.description": "Soluciones para redes Wi-Fi restrictivas.",
  "settings.network.allowLan.title": "Permitir LAN",
  "settings.network.allowLan.hint": "Permite que el tráfico de la red local (router, impresoras, portales cautivos) evite el túnel. Actívalo si tu Wi-Fi cae de forma intermitente a \"Sin conexión\" mientras estás conectado. Se aplica en la próxima conexión.",
  "settings.startup.heading": "Inicio",
  "settings.startup.description": "Inicio en segundo plano y reconexión automática.",
  "settings.startup.launch.title": "Iniciar al arrancar",
  "settings.startup.launch.hint": "Inicia PangeaVPN automáticamente al iniciar sesión. Se abre oculto — usa el icono de la bandeja para acceder.",
  "settings.startup.lockdown.title": "Modo bloqueo",
  "settings.startup.lockdown.hint": "Inicia PangeaVPN al arrancar (oculto en la bandeja), se conecta automáticamente al último servidor y mantiene el Kill Switch activado después de desconectarte — solo se permite el tráfico de la VPN. Desactiva el Modo bloqueo para recuperar el acceso a internet sin protección.",
  "settings.language.heading": "Idioma",
  "settings.language.description": "Elige el idioma de la aplicación.",
  "settings.language.system": "Predeterminado del sistema",
  "settings.language.restartHint": "Reinicia PangeaVPN para aplicar el nuevo idioma.",
  "settings.update.heading": "Actualización de software",
  "settings.update.currentVersion": "Versión actual:",
  "settings.update.check": "Buscar actualizaciones",

  // ── Server picker overlay ─────────────────────────────
  "serverPicker.title": "Seleccionar servidor",
  "serverPicker.serversAriaLabel": "Servidores",
  "serverPicker.noServers": "No hay servidores disponibles",
  "serverPicker.load": "Carga del servidor {pct}%",
  "serverPicker.loadPct": "{pct}%",

  // ── Update modal ──────────────────────────────────────
  "update.title": "Actualización disponible",
  "update.current": "Actual",
  "update.latest": "Última",
  "update.macStep": "Abre Terminal (pulsa ⌘ + Espacio, escribe Terminal y pulsa Intro), luego pega este comando y pulsa Intro:",
  "update.download": "Descargar actualización",
  "update.copyCommand": "Copiar comando de instalación",
  "update.copied": "¡Copiado!",
  "update.macPasteHint": "Ahora pega el comando en Terminal y pulsa Intro.",
  "update.restartToUpdate": "Reiniciar para actualizar",
  "update.readyToInstall": "Actualización descargada y lista para instalar.",
  "update.opening": "Abriendo...",
  "update.viewDownload": "Ver descarga",
  "update.retry": "Reintentar",
  "update.retryDownload": "Reintentar descarga",
  "update.checking": "Comprobando...",
  "update.onLatest": "Ya tienes la última versión.",
  "update.checkFailed": "No se pudieron buscar actualizaciones. Inténtalo más tarde.",
  "update.unavailable": "Las actualizaciones no están disponibles en esta versión.",

  // ── Account / subscription ────────────────────────────
  "sub.none": "Sin suscripción activa",
  "sub.trialPrefix": "Prueba gratis · ",
  "sub.renews": "Se renueva",
  "sub.expires": "Caduca",
  "sub.pastDue": "Pago vencido",
  "account.noToken": "No hay token para copiar.",
  "account.tokenCopied": "Token copiado al portapapeles.",

  // ── Session / auth toasts ─────────────────────────────
  "auth.signedOutRetry": "Se cerró tu sesión. Inicia sesión de nuevo.",
  "auth.signingOut": "Cerrando sesión...",
  "auth.signedOut": "Sesión cerrada.",
  "auth.deviceNamed": "Tu dispositivo se llama \"{name}\".",

  // ── Connect / disconnect flow ─────────────────────────
  "connect.noServer": "Ningún servidor seleccionado.",
  "connect.provisioning": "Aprovisionando y conectando...",
  "connect.connected": "Conectado.",
  "connect.failed": "No se pudo conectar. Inténtalo de nuevo o elige otro servidor.",
  "connect.switching": "Aprovisionando el nuevo servidor...",
  "connect.switchFailed": "No se pudo cambiar de servidor. Inténtalo de nuevo o elige otro servidor.",
  "connect.disconnecting": "Desconectando...",
  "connect.disconnected": "Desconectado.",
  "connect.disconnectFailed": "No se pudo desconectar. Inténtalo de nuevo.",
  "connect.recovered": "Conexión recuperada.",
  "connect.refreshServersFailed": "No se pudo actualizar la lista de servidores. Se reintentará automáticamente.",

  // ── Settings toggles ──────────────────────────────────
  "toggle.updateFailed": "No se pudo actualizar el ajuste.",
  "toggle.directIp.on": "IP directa activada.",
  "toggle.directIp.off": "IP directa desactivada.",
  "toggle.directIpOnly.on": "Modo solo IP directa activado.",
  "toggle.directIpOnly.off": "Modo solo IP directa desactivado.",
  "toggle.allowLan.on": "Permitir LAN activado. Vuelve a conectar para que surta efecto.",
  "toggle.allowLan.off": "Permitir LAN desactivado. Vuelve a conectar para que surta efecto.",
  "toggle.preferredTransport.updated": "Connection method updated. Reconnect for it to take effect.",
  "toggle.launch.on": "PangeaVPN se iniciará al arrancar.",
  "toggle.launch.off": "Inicio al arrancar desactivado.",
  "toggle.launch.failed": "No se pudo actualizar el ajuste de inicio.",
  "toggle.launch.packagedOnly": "Disponible solo en versiones empaquetadas",
  "toggle.lockdown.failed": "No se pudo actualizar el Modo bloqueo.",
  "toggle.lockdown.on": "Modo bloqueo activado — conexión automática activada y el Kill Switch permanece activo hasta que lo desactives.",
  "toggle.lockdown.off": "Modo bloqueo desactivado — acceso a internet normal restaurado.",

  // ── Verbose errors (hidden debug toggle) ──────────────
  "verbose.on": "Errores detallados activados",
  "verbose.off": "Errores detallados desactivados",

  // ── Daemon sync / generic ─────────────────────────────
  "common.loading": "Cargando...",
  "common.ready": "Listo.",
  "common.retrying": "Algo salió mal. Reintentando...",
  "common.dash": "—",

  // ── Daemon status values (technical, kept short) ──────
  "status.running": "en ejecución",
  "status.stopped": "detenido",
  "status.transport.cloak": "Obfuscation: Cloak",
  "status.transport.naive": "Obfuscation: NaiveProxy",
  "status.transport.hysteria2": "Obfuscation: Hysteria2",
  "status.transport.none": "",

  // ── Generic error fallbacks ───────────────────────────
  "error.network": "Comprueba tu conexión a internet e inténtalo de nuevo.",
  "error.generic": "Algo salió mal. Inténtalo más tarde."
} satisfies Messages;
