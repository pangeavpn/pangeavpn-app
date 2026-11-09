package org.pangeavpn.core.tunnel

/** Android's Always-on VPN for this app, and its "Block connections without VPN". */
data class SystemVpnState(val alwaysOn: Boolean, val blockWithoutVpn: Boolean)
