package org.pangeavpn.app

import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import androidx.core.content.pm.ShortcutInfoCompat
import androidx.core.content.pm.ShortcutManagerCompat
import androidx.core.graphics.drawable.IconCompat
import java.util.UUID
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.PanicWiper
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.R as UiR

private const val SHORTCUT_ID = "panic-wipe"
private const val EXTRA_TOKEN = "org.pangeavpn.app.extra.PANIC_TOKEN"
private const val KEY_TOKEN = "panicToken"

/** The shell's own small prefs; a panic wipe clears them with every other preference file. */
internal fun shellPrefs(context: Context): SharedPreferences =
    context.getSharedPreferences("pangea_shell", Context.MODE_PRIVATE)

/** The launcher's "Wipe data" shortcut and the wipe it starts; its label stays neutral on purpose. */
object Panic {
    const val ACTION = "org.pangeavpn.app.action.PANIC_WIPE"

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var running: Job? = null

    /** Re-applied on every launch, so the label follows the language. */
    fun syncShortcut(context: Context) {
        val prefs = shellPrefs(context)
        if (!AppPrefs.value.panicWipe) {
            ShortcutManagerCompat.removeDynamicShortcuts(context, listOf(SHORTCUT_ID))
            prefs.edit().remove(KEY_TOKEN).apply()
            return
        }
        val token = prefs.getString(KEY_TOKEN, null)
            ?: UUID.randomUUID().toString().also { prefs.edit().putString(KEY_TOKEN, it).apply() }
        val shortcut = ShortcutInfoCompat.Builder(context, SHORTCUT_ID)
            .setShortLabel(context.getString(UiR.string.panic_shortcut_title))
            .setIcon(IconCompat.createWithResource(context, R.drawable.ic_shortcut_wipe))
            .setIntent(Intent(ACTION, null, context, MainActivity::class.java).putExtra(EXTRA_TOKEN, token))
            .build()
        ShortcutManagerCompat.addDynamicShortcuts(context, listOf(shortcut))
    }

    /** Concurrent requests share one wipe; the root shows the splash meanwhile. */
    fun wipeNow(context: Context) {
        if (running?.isActive == true) return
        val app = context.applicationContext
        running = scope.launch {
            PanicWiper.wipe(app)
            syncShortcut(app)
            ToastCenter.show(app.getString(UiR.string.root_data_erased), ToastStyle.SUCCESS)
        }
    }

    // MainActivity is exported, so only an intent carrying this install's token may wipe.
    internal fun requested(context: Context, intent: Intent?): Boolean {
        if (intent?.action != ACTION || !AppPrefs.value.panicWipe) return false
        val token = shellPrefs(context).getString(KEY_TOKEN, null) ?: return false
        return token == intent?.getStringExtra(EXTRA_TOKEN)
    }
}
