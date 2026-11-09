package org.pangeavpn.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.unit.dp
import org.pangeavpn.ui.components.BrandLogo

/** Logo only: shown while the launch restore settles and while a panic wipe runs. */
@Composable
fun SplashScreen() {
    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            .clearAndSetSemantics {},
        contentAlignment = Alignment.Center,
    ) {
        BrandLogo(size = 72.dp)
    }
}
