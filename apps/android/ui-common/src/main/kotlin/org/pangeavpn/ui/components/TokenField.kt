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
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.OffsetMapping
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.text.input.TransformedText
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextDirection
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.core.util.formatAccountNumberInput
import org.pangeavpn.core.util.sanitizeAccountNumberInput
import org.pangeavpn.ui.R
import org.pangeavpn.ui.theme.StateError

/** The account-number field. It holds only the typed symbols, draws the dashes itself and reports the
 *  formatted text. Memory only: the number is a credential, so no saved state. */
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
    val incoming = symbolsOf(value)
    var field by remember { mutableStateOf(TextFieldValue(incoming, TextRange(incoming.length))) }
    // Letters are only drawn upper case, so the caller's upper-cased echo of the buffer is the same value.
    val shown = if (field.text.equals(incoming, ignoreCase = true)) field else TextFieldValue(incoming, TextRange(incoming.length))
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
                    val edited = kept(next)
                    field = edited
                    val formatted = formatAccountNumberInput(edited.text)
                    if (formatted != value) onValueChange(formatted)
                }
            },
            visualTransformation = AccountNumberGroups,
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

// Letters and digits only, capped at 64: a typed or pasted dash, space or anything else is dropped.
private fun symbolsOf(text: String): String = sanitizeAccountNumberInput(text).filter { it != '-' }

// A hardware Return types a newline into a wrapping field; a paste that ends in one is not a submit.
private fun isReturnPress(old: String, new: String): Boolean {
    val stripped = new.filterNot { it == '\n' || it == '\r' }
    return stripped != new && stripped == old
}

// Untouched unless something was dropped, so the IME's buffer and the field agree through a fast burst.
private fun kept(next: TextFieldValue): TextFieldValue {
    val symbols = symbolsOf(next.text)
    if (symbols == next.text) return next
    fun mapped(offset: Int): Int = symbolsOf(next.text.take(offset)).length.coerceAtMost(symbols.length)
    return TextFieldValue(symbols, TextRange(mapped(next.selection.start), mapped(next.selection.end)))
}

// A dash after every fourth symbol, drawn only; a legacy all-digit token shows as typed.
private object AccountNumberGroups : VisualTransformation {
    private const val GROUP = 4

    override fun filter(text: AnnotatedString): TransformedText {
        val raw = text.text
        if (raw.all { it in '0'..'9' }) return TransformedText(text, OffsetMapping.Identity)
        val grouped = raw.uppercase().chunked(GROUP).joinToString("-")
        val offsets = object : OffsetMapping {
            override fun originalToTransformed(offset: Int): Int =
                if (offset >= raw.length) grouped.length else offset + offset / GROUP

            override fun transformedToOriginal(offset: Int): Int =
                (offset - offset / (GROUP + 1)).coerceIn(0, raw.length)
        }
        return TransformedText(AnnotatedString(grouped), offsets)
    }
}
