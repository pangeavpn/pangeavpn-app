package org.pangeavpn.ui.components

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import org.pangeavpn.ui.R

/** Connection-method choices. NaiveProxy is absent: see TRANSPORT_CHOICES. */
@Composable
fun transportChoiceLabel(kind: String): String = when (kind) {
    "auto" -> stringResource(R.string.settings_transport_auto)
    "cloak" -> stringResource(R.string.settings_transport_cloak)
    "reality" -> stringResource(R.string.settings_transport_reality)
    "shadowsocks" -> stringResource(R.string.settings_transport_shadowsocks)
    "hysteria2" -> stringResource(R.string.settings_transport_hysteria2)
    "anytls" -> stringResource(R.string.settings_transport_anytls)
    "snowflake" -> stringResource(R.string.settings_transport_snowflake)
    else -> kind
}

/** What each connection method does, under its name on the Censorship Bypass page. */
@Composable
fun transportChoiceDescription(kind: String): String = when (kind) {
    "auto" -> stringResource(R.string.bypass_auto_desc)
    "cloak" -> stringResource(R.string.bypass_cloak_desc)
    "reality" -> stringResource(R.string.bypass_reality_desc)
    "shadowsocks" -> stringResource(R.string.bypass_shadowsocks_desc)
    "hysteria2" -> stringResource(R.string.bypass_hysteria2_desc)
    "anytls" -> stringResource(R.string.bypass_anytls_desc)
    else -> ""
}

@Composable
fun hubMethodTitle(method: String): String = when (method) {
    "directIp" -> stringResource(R.string.settings_censorship_directip_title)
    "reality" -> stringResource(R.string.settings_censorship_reality_title)
    "shadowsocks" -> stringResource(R.string.settings_censorship_shadowsocks_title)
    "fronted" -> stringResource(R.string.settings_censorship_fronted_title)
    "normal" -> stringResource(R.string.settings_censorship_normal_title)
    else -> method
}

@Composable
fun hubMethodHint(method: String): String = when (method) {
    "directIp" -> stringResource(R.string.settings_censorship_directip_hint)
    "reality" -> stringResource(R.string.settings_censorship_reality_hint)
    "shadowsocks" -> stringResource(R.string.settings_censorship_shadowsocks_hint)
    "fronted" -> stringResource(R.string.settings_censorship_fronted_hint)
    "normal" -> stringResource(R.string.settings_censorship_normal_hint)
    else -> ""
}
