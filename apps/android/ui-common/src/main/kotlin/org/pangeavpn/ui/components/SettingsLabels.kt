package org.pangeavpn.ui.components

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import org.pangeavpn.ui.R

/** Connection-method choices. NaiveProxy is absent: it cannot run on Android. */
@Composable
fun transportChoiceLabel(kind: String): String = when (kind) {
    "auto" -> stringResource(R.string.settings_transport_auto)
    "cloak" -> stringResource(R.string.settings_transport_cloak)
    "reality" -> stringResource(R.string.settings_transport_reality)
    "shadowsocks" -> stringResource(R.string.settings_transport_shadowsocks)
    "hysteria2" -> stringResource(R.string.settings_transport_hysteria2)
    "snowflake" -> stringResource(R.string.settings_transport_snowflake)
    else -> kind
}

@Composable
fun hubMethodTitle(method: String): String = when (method) {
    "directIp" -> stringResource(R.string.settings_censorship_directip_title)
    "shadowsocks" -> stringResource(R.string.settings_censorship_shadowsocks_title)
    "fronted" -> stringResource(R.string.settings_censorship_fronted_title)
    "normal" -> stringResource(R.string.settings_censorship_normal_title)
    else -> method
}

@Composable
fun hubMethodHint(method: String): String = when (method) {
    "directIp" -> stringResource(R.string.settings_censorship_directip_hint)
    "shadowsocks" -> stringResource(R.string.settings_censorship_shadowsocks_hint)
    "fronted" -> stringResource(R.string.settings_censorship_fronted_hint)
    "normal" -> stringResource(R.string.settings_censorship_normal_hint)
    else -> ""
}
