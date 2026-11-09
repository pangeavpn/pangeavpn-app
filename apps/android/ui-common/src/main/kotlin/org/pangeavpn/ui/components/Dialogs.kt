package org.pangeavpn.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import org.pangeavpn.ui.R
import org.pangeavpn.ui.theme.LocalTextSecondary

private val DialogShape = RoundedCornerShape(20.dp)

/** The app's dialog card: a title over whatever is stacked beneath it (a field, buttons).
 *  Back and a tap outside call onDismiss. */
@Composable
fun PangeaDialog(
    title: String,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable ColumnScope.() -> Unit,
) {
    Dialog(onDismissRequest = onDismiss) {
        Column(
            modifier = modifier
                .widthIn(max = 420.dp)
                .fillMaxWidth()
                .clip(DialogShape)
                .background(MaterialTheme.colorScheme.surface)
                .border(width = 1.dp, color = MaterialTheme.colorScheme.outline, shape = DialogShape)
                .verticalScroll(rememberScrollState())
                .padding(20.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(
                text = title,
                modifier = Modifier.semantics { heading() },
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 20.sp,
                lineHeight = 26.sp,
                fontWeight = FontWeight.SemiBold,
            )
            content()
        }
    }
}

/** Asks before something that matters. Either button closes it through onDismiss, and
 *  Confirm then runs onConfirm; destructive paints Confirm red. */
@Composable
fun ConfirmDialog(
    title: String,
    message: String,
    confirmLabel: String,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
    destructive: Boolean = false,
    cancelLabel: String = stringResource(R.string.settings_home_cancel),
) {
    PangeaDialog(title = title, onDismiss = onDismiss) {
        Text(
            text = message,
            modifier = Modifier.padding(bottom = 4.dp),
            color = LocalTextSecondary.current,
            fontSize = 14.sp,
            lineHeight = 20.sp,
        )
        PillButton(
            label = confirmLabel,
            onClick = {
                onDismiss()
                onConfirm()
            },
            destructive = destructive,
        )
        GhostButton(label = cancelLabel, onClick = onDismiss)
    }
}
