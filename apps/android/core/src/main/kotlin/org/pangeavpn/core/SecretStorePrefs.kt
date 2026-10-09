package org.pangeavpn.core

import android.content.Context
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import org.pangeavpn.mobile.SecretStore

private const val PREFS_FILE = "pangea_secure"

class SecretStorePrefs(context: Context) : SecretStore {
    private val prefs = EncryptedSharedPreferences.create(
        context,
        PREFS_FILE,
        MasterKey.Builder(context).setKeyScheme(MasterKey.KeyScheme.AES256_GCM).build(),
        EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
        EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
    )

    override fun get(key: String): String = prefs.getString(key, "") ?: ""

    override fun set(key: String, value: String) {
        prefs.edit().putString(key, value).apply()
    }
}
