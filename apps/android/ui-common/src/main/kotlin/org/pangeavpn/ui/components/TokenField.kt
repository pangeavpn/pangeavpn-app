package org.pangeavpn.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.ui.theme.StateError

private val FieldShape = RoundedCornerShape(14.dp)

/** The token input, wearing the region row's card instead of Material's default text
 *  field, so the one thing there is to fill in belongs to the same app as the rest. */
@Composable
fun TokenField(
    value: String,
    onValueChange: (String) -> Unit,
    placeholder: String,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    isError: Boolean = false,
    onSubmit: () -> Unit = {},
) {
    var focused by remember { mutableStateOf(false) }

    val textStyle = TextStyle(
        color = MaterialTheme.colorScheme.onSurface,
        fontFamily = FontFamily.Monospace,
        fontSize = 17.sp,
        letterSpacing = 2.4.sp,
    )

    BasicTextField(
        value = value,
        // A pasted token carries stray spaces and newlines far more often than it contains one.
        onValueChange = { onValueChange(it.filterNot(Char::isWhitespace)) },
        modifier = modifier
            .fillMaxWidth()
            .clip(FieldShape)
            .background(
                if (focused) MaterialTheme.colorScheme.primary.copy(alpha = 0.1f)
                else MaterialTheme.colorScheme.surface,
            )
            .border(
                width = if (focused) 2.dp else 1.dp,
                color = when {
                    isError -> StateError
                    focused -> MaterialTheme.colorScheme.primary
                    else -> MaterialTheme.colorScheme.outlineVariant
                },
                shape = FieldShape,
            )
            .onFocusChanged { focused = it.isFocused },
        enabled = enabled,
        textStyle = textStyle,
        singleLine = true,
        cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
        keyboardOptions = KeyboardOptions(
            capitalization = KeyboardCapitalization.None,
            autoCorrectEnabled = false,
            keyboardType = KeyboardType.Ascii,
            imeAction = ImeAction.Go,
        ),
        keyboardActions = KeyboardActions(onGo = { onSubmit() }),
        decorationBox = { field ->
            Box(modifier = Modifier.padding(horizontal = 16.dp, vertical = 15.dp)) {
                if (value.isEmpty()) {
                    Text(
                        text = placeholder,
                        style = textStyle.copy(color = MaterialTheme.colorScheme.onSurfaceVariant),
                    )
                }
                field()
            }
        },
    )
}
