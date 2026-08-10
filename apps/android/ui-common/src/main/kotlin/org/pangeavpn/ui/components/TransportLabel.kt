package org.pangeavpn.ui.components

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import org.pangeavpn.ui.R

/** Names whichever cascade rung carried the tunnel, so the VIA fact reflects the
 *  live transport rather than always naming Cloak. */
@Composable
fun transportStatusLabel(activeTransport: String): String = when (activeTransport) {
    "cloak" -> stringResource(R.string.transport_status_cloak)
    "reality" -> stringResource(R.string.transport_status_reality)
    "shadowsocks" -> stringResource(R.string.transport_status_shadowsocks)
    "hysteria2" -> stringResource(R.string.transport_status_hysteria2)
    "snowflake" -> stringResource(R.string.transport_status_snowflake)
    // NaiveProxy never runs here, but the hub may still name it.
    else -> stringResource(R.string.transport_status_none)
}
