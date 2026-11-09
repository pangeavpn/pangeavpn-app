package org.pangeavpn.core.session

import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.Prefs

enum class RootScreen { SPLASH, DATA_NOTICE, DEVICE_LIMIT, RESTORE_FAILED, LOGIN, MAIN }

/** Which screen the app is on; the data notice comes before sign-in, even signed out. */
fun rootScreen(
    restored: Boolean,
    erasing: Boolean,
    noticeAccepted: Boolean,
    showDeviceLimit: Boolean,
    signedIn: Boolean,
    restoreFailed: Boolean,
): RootScreen = when {
    !restored || erasing -> RootScreen.SPLASH
    !noticeAccepted -> RootScreen.DATA_NOTICE
    showDeviceLimit -> RootScreen.DEVICE_LIMIT
    !signedIn -> if (restoreFailed) RootScreen.RESTORE_FAILED else RootScreen.LOGIN
    else -> RootScreen.MAIN
}

/** Whether this device has seen the data notice; raising the version shows it again after a change. */
object DataNotice {
    const val VERSION = 1

    fun isAccepted(prefs: Prefs): Boolean = prefs.dataNoticeVersion >= VERSION

    fun accept() = AppPrefs.update { it.copy(dataNoticeVersion = VERSION) }
}
