package org.pangeavpn.ui.components

import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.size
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ColorFilter
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import org.pangeavpn.ui.R

/** The Africa silhouette, tinted with the current accent: the same asset the desktop
 *  header masks, so the two wordmarks are the same shape. */
@Composable
fun BrandLogo(
    size: Dp = 32.dp,
    tint: Color = MaterialTheme.colorScheme.primary,
    modifier: Modifier = Modifier,
) {
    Image(
        painter = painterResource(R.drawable.logo_africa),
        contentDescription = stringResource(R.string.app_name),
        modifier = modifier.size(size),
        colorFilter = ColorFilter.tint(tint),
    )
}
