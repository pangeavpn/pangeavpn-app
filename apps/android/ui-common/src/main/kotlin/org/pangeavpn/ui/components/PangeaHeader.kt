package org.pangeavpn.ui.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.DarkMode
import androidx.compose.material.icons.outlined.LightMode
import androidx.compose.material.icons.rounded.Menu
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.ui.R
import org.pangeavpn.ui.theme.LocalAppearance

/** Brand mark, version, and the icon controls. As on desktop, the theme icon names the theme a
 *  tap switches to. onMenu is optional because sign-in may have nothing behind the burger. */
@Composable
fun PangeaHeader(
    version: String,
    onMenu: (() -> Unit)? = null,
    onToggleTheme: () -> Unit = LocalAppearance.current::toggle,
    modifier: Modifier = Modifier,
) {
    val dark = LocalAppearance.current.dark

    Row(
        modifier = modifier
            .fillMaxWidth()
            .height(56.dp)
            .padding(start = 20.dp, end = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        BrandLogo()
        Text(
            text = version,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            fontSize = 12.sp,
            maxLines = 1,
        )
        Spacer(Modifier.weight(1f))
        IconAction(
            icon = if (dark) Icons.Outlined.LightMode else Icons.Outlined.DarkMode,
            label = stringResource(R.string.header_toggle_theme),
            onClick = onToggleTheme,
        )
        if (onMenu != null) {
            IconAction(
                icon = Icons.Rounded.Menu,
                label = stringResource(R.string.header_menu),
                onClick = onMenu,
            )
        }
    }
}
