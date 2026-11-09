package org.pangeavpn.app.ui.settings

import android.Manifest
import android.app.Activity
import android.app.KeyguardManager
import android.content.Context
import android.content.pm.PackageManager
import android.hardware.biometrics.BiometricManager
import android.hardware.biometrics.BiometricPrompt
import android.os.Build
import android.os.CancellationSignal
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.annotation.RequiresApi
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import java.util.concurrent.atomic.AtomicReference
import kotlin.coroutines.resume
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.suspendCancellableCoroutine
import org.pangeavpn.ui.R

/** The device-owner check before the account number shows: true when the owner passed, or when the
 *  device has no secure lock to pass. A cancel or any other failure is false. */
@Composable
internal fun rememberOwnerCheck(): suspend () -> Boolean {
    val context = LocalContext.current
    val title = stringResource(R.string.settings_account_number)
    val reason = stringResource(R.string.session_reveal_reason)
    val answer = remember { AtomicReference<CompletableDeferred<Boolean>?>(null) }
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) {
        answer.get()?.complete(it.resultCode == Activity.RESULT_OK)
    }
    return remember(context, title, reason) {
        suspend {
            val keyguard = context.getSystemService(Context.KEYGUARD_SERVICE) as KeyguardManager
            when {
                !keyguard.isDeviceSecure -> true
                Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q && mayPrompt(context) ->
                    promptPassed(context, title, reason)
                else -> {
                    @Suppress("DEPRECATION")
                    val intent = keyguard.createConfirmDeviceCredentialIntent(title, reason)
                    if (intent == null) {
                        true
                    } else {
                        val result = CompletableDeferred<Boolean>()
                        answer.set(result)
                        launcher.launch(intent)
                        result.await()
                    }
                }
            }
        }
    }
}

// The framework prompt offers the PIN as well from API 29; without USE_BIOMETRIC it would throw.
private fun mayPrompt(context: Context): Boolean =
    context.checkSelfPermission(Manifest.permission.USE_BIOMETRIC) == PackageManager.PERMISSION_GRANTED

@RequiresApi(Build.VERSION_CODES.Q)
private suspend fun promptPassed(context: Context, title: String, reason: String): Boolean =
    suspendCancellableCoroutine { done ->
        val builder = BiometricPrompt.Builder(context).setTitle(title).setSubtitle(reason)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            builder.setAllowedAuthenticators(
                BiometricManager.Authenticators.BIOMETRIC_WEAK or BiometricManager.Authenticators.DEVICE_CREDENTIAL,
            )
        } else {
            @Suppress("DEPRECATION")
            builder.setDeviceCredentialAllowed(true)
        }
        val cancel = CancellationSignal()
        done.invokeOnCancellation { cancel.cancel() }
        val callback = object : BiometricPrompt.AuthenticationCallback() {
            override fun onAuthenticationSucceeded(result: BiometricPrompt.AuthenticationResult?) {
                if (done.isActive) done.resume(true)
            }

            // A failed attempt keeps the prompt open; only an error ends it.
            override fun onAuthenticationError(errorCode: Int, errString: CharSequence?) {
                if (done.isActive) done.resume(errorCode == BiometricPrompt.BIOMETRIC_ERROR_NO_DEVICE_CREDENTIAL)
            }
        }
        builder.build().authenticate(cancel, context.mainExecutor, callback)
    }
