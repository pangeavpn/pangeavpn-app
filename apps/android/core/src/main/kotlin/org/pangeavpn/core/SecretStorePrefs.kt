package org.pangeavpn.core

import android.content.Context
import android.content.SharedPreferences
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import java.security.KeyStore
import org.pangeavpn.mobile.SecretStore

internal const val SECRET_PREFS_FILE = "pangea_secure"

/** The Go core's SecretStore, encrypted under a Keystore key. Go calls it from its own threads. */
class SecretStorePrefs(context: Context) : SecretStore {
    private val prefs = open(context.applicationContext)

    // A value that no longer decrypts reads as absent, as a missing Keychain item does on iOS.
    override fun get(key: String): String = runCatching { prefs.getString(key, "") }.getOrNull().orEmpty()

    // Committed, not applied: a process killed straight after sign-in must not lose the account.
    override fun set(key: String, value: String) {
        val editor = prefs.edit()
        if (value.isEmpty()) editor.remove(key) else editor.putString(key, value)
        editor.commit()
    }

    /** Removes every secret. The keysets stay, so the instance the core holds keeps working. */
    fun clear() {
        prefs.edit().clear().commit()
    }

    companion object {
        /** For the panic wipe. */
        fun wipe(context: Context) {
            SecretStorePrefs(context).clear()
        }

        // A damaged keystore makes create throw on every launch; starting over signs the user out, the honest outcome.
        private fun open(app: Context): SharedPreferences =
            runCatching { create(app) }.getOrElse {
                app.deleteSharedPreferences(SECRET_PREFS_FILE)
                runCatching {
                    KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
                        .deleteEntry(MasterKey.DEFAULT_MASTER_KEY_ALIAS)
                }
                create(app)
            }

        private fun create(app: Context): SharedPreferences = EncryptedSharedPreferences.create(
            app,
            SECRET_PREFS_FILE,
            MasterKey.Builder(app).setKeyScheme(MasterKey.KeyScheme.AES256_GCM).build(),
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
        )
    }
}
