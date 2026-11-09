package org.pangeavpn.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.text.style.TextDirection
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.core.util.formatAccountNumberInput
import org.pangeavpn.core.util.sanitizeAccountNumberInput
import org.pangeavpn.ui.R
import org.pangeavpn.ui.theme.StateError

/** The account-number field. It sanitizes and groups every edit itself, keeps the caret beside what was
 *  typed, and reports the formatted text. Memory only: the number is a credential, so no saved state. */
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
    var field by remember { mutableStateOf(TextFieldValue(value, TextRange(value.length))) }
    val shown = if (field.text == value) field else TextFieldValue(value, TextRange(value.length))
    var focused by remember { mutableStateOf(false) }
    val keyboard = LocalSoftwareKeyboardController.current
    val label = stringResource(R.string.login_account_label)
    val submit = {
        keyboard?.hide()
        onSubmit()
    }

    val textStyle = TextStyle(
        color = MaterialTheme.colorScheme.onSurface,
        fontFamily = FontFamily.Monospace,
        fontSize = 17.sp,
        letterSpacing = 2.4.sp,
        textDirection = TextDirection.Ltr,
    )

    CompositionLocalProvider(LocalLayoutDirection provides LayoutDirection.Ltr) {
        BasicTextField(
            value = shown,
            onValueChange = { next ->
                if (isReturnPress(shown.text, next.text)) {
                    submit()
                } else {
                    val edited = reformatted(next)
                    field = edited
                    if (edited.text != value) onValueChange(edited.text)
                }
            },
            modifier = modifier
                .fillMaxWidth()
                .clip(PangeaCardShape)
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
                    shape = PangeaCardShape,
                )
                .onFocusChanged { focused = it.isFocused }
                .semantics { contentDescription = label },
            enabled = enabled,
            textStyle = textStyle,
            // A full number wraps on a phone; reserving two lines stops the field jumping as it does.
            singleLine = false,
            minLines = 2,
            cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
            keyboardOptions = KeyboardOptions(
                capitalization = KeyboardCapitalization.Characters,
                autoCorrectEnabled = false,
                keyboardType = KeyboardType.Ascii,
                imeAction = ImeAction.Go,
            ),
            keyboardActions = KeyboardActions(onGo = { submit() }),
            decorationBox = { inner ->
                Box(modifier = Modifier.padding(horizontal = 16.dp, vertical = 15.dp)) {
                    if (shown.text.isEmpty()) {
                        Text(
                            text = placeholder,
                            modifier = Modifier.clearAndSetSemantics {},
                            style = textStyle.copy(color = MaterialTheme.colorScheme.onSurfaceVariant),
                            maxLines = 2,
                        )
                    }
                    inner()
                }
            },
        )
    }
}

private fun isSymbol(c: Char): Boolean = c in '0'..'9' || c in 'A'..'Z' || c in 'a'..'z'

// A hardware Return types a newline into a wrapping field; a paste that ends in one is not a submit.
private fun isReturnPress(old: String, new: String): Boolean {
    val stripped = new.filterNot { it == '\n' || it == '\r' }
    return stripped != new && stripped == old
}

// The caret lands after as many symbols as were before it, so inserted dashes never push it back.
private fun reformatted(next: TextFieldValue): TextFieldValue {
    val formatted = formatAccountNumberInput(sanitizeAccountNumberInput(next.text))
    if (formatted == next.text) return next
    val symbols = next.text.take(next.selection.end).count(::isSymbol)
    return TextFieldValue(formatted, TextRange(caretAfter(formatted, symbols)))
}

private fun caretAfter(text: String, symbols: Int): Int {
    if (symbols == 0) return 0
    var seen = 0
    text.forEachIndexed { index, c ->
        if (c != '-' && ++seen == symbols) return index + 1
    }
    return text.length
}
