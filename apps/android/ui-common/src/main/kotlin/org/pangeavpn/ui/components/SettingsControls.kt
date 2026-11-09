package org.pangeavpn.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.KeyboardArrowRight
import androidx.compose.material.icons.rounded.Check
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import org.pangeavpn.ui.R
import org.pangeavpn.ui.theme.LocalTextSecondary

/** The 14 dp corner every card, row and field shares. */
val PangeaCardShape = RoundedCornerShape(14.dp)

/** The settings surfaces reuse the region row's card so a page reads as the
 *  same app as the hero it opened from, not as stock Material. */
@Composable
internal fun Modifier.cardSurface(selected: Boolean = false, focused: Boolean = false): Modifier =
    clip(PangeaCardShape)
        .background(
            if (selected) MaterialTheme.colorScheme.primary.copy(alpha = 0.15f)
            else MaterialTheme.colorScheme.surface,
        )
        .border(
            width = if (focused) 2.dp else 1.dp,
            color = when {
                focused || selected -> MaterialTheme.colorScheme.primary
                else -> MaterialTheme.colorScheme.outlineVariant
            },
            shape = PangeaCardShape,
        )

/** The plain card, for anything the ready-made rows do not cover. With onClick the whole
 *  card is one button; padding = PaddingValues(0.dp) lets rows inside run edge to edge. */
@Composable
fun PangeaCard(
    padding: PaddingValues = PaddingValues(14.dp),
    selected: Boolean = false,
    onClick: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
    content: @Composable ColumnScope.() -> Unit,
) {
    var focused by remember { mutableStateOf(false) }

    Column(
        modifier = modifier
            .fillMaxWidth()
            .cardSurface(selected = selected, focused = focused)
            .then(
                if (onClick == null) {
                    Modifier
                } else {
                    Modifier
                        .onFocusChanged { focused = it.isFocused }
                        .clickable(role = Role.Button, onClick = onClick)
                },
            )
            .padding(padding),
        content = content,
    )
}

/** A stack of rows under an optional small-caps heading, 9 dp apart: one section of a page. */
@Composable
fun SettingsGroup(
    title: String? = null,
    modifier: Modifier = Modifier,
    content: @Composable ColumnScope.() -> Unit,
) {
    Column(
        modifier = modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(9.dp),
    ) {
        if (title != null) {
            SectionLabel(text = title, modifier = Modifier.padding(start = 4.dp).semantics { heading() })
        }
        content()
    }
}

/** A settings-home row: icon, title, muted monospaced summary and a chevron. tint reddens a
 *  destructive row such as Sign Out; loading dims it and swaps the chevron for a spinner. */
@Composable
fun SettingsNavRow(
    icon: ImageVector,
    title: String,
    onClick: () -> Unit,
    summary: String? = null,
    tint: Color? = null,
    showChevron: Boolean = true,
    loading: Boolean = false,
    modifier: Modifier = Modifier,
) {
    var focused by remember { mutableStateOf(false) }

    Row(
        modifier = modifier
            .fillMaxWidth()
            .alpha(if (loading) 0.5f else 1f)
            .cardSurface(focused = focused)
            .onFocusChanged { focused = it.isFocused }
            .clickable(role = Role.Button) { if (!loading) onClick() }
            .heightIn(min = 56.dp)
            .padding(horizontal = 14.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            modifier = Modifier.size(22.dp),
            tint = tint ?: LocalTextSecondary.current,
        )
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = title,
                color = tint ?: MaterialTheme.colorScheme.onSurface,
                fontSize = 16.sp,
                fontWeight = FontWeight.SemiBold,
            )
            if (!summary.isNullOrEmpty()) {
                Text(
                    text = summary,
                    modifier = Modifier.padding(top = 2.dp),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    fontFamily = FontFamily.Monospace,
                    fontSize = 12.sp,
                    lineHeight = 16.sp,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        if (loading) {
            CircularProgressIndicator(
                modifier = Modifier.size(18.dp),
                color = tint ?: MaterialTheme.colorScheme.onSurfaceVariant,
                strokeWidth = 2.dp,
            )
        } else if (showChevron) {
            Icon(
                imageVector = Icons.AutoMirrored.Rounded.KeyboardArrowRight,
                contentDescription = null,
                modifier = Modifier.size(20.dp),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun RowText(title: String, hint: String?, modifier: Modifier = Modifier) {
    Column(modifier = modifier) {
        Text(
            text = title,
            color = MaterialTheme.colorScheme.onSurface,
            fontSize = 15.sp,
            fontWeight = FontWeight.SemiBold,
        )
        if (!hint.isNullOrEmpty()) {
            Text(
                text = hint,
                modifier = Modifier.padding(top = 2.dp),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontSize = 12.sp,
                lineHeight = 16.sp,
            )
        }
    }
}

/** One option in a mutually exclusive set, ticked like the region it echoes. hint is the
 *  optional description under the label. */
@Composable
fun ChoiceRow(
    label: String,
    selected: Boolean,
    onClick: () -> Unit,
    hint: String? = null,
    enabled: Boolean = true,
    modifier: Modifier = Modifier,
) {
    var focused by remember { mutableStateOf(false) }

    Row(
        modifier = modifier
            .fillMaxWidth()
            .alpha(if (enabled) 1f else 0.6f)
            .cardSurface(selected = selected, focused = focused)
            .onFocusChanged { focused = it.isFocused }
            .selectable(selected = selected, enabled = enabled, role = Role.RadioButton, onClick = onClick)
            .heightIn(min = 48.dp)
            .padding(horizontal = 14.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        RowText(title = label, hint = hint, modifier = Modifier.weight(1f))
        // Always laid out so the rows either side of a selection stay aligned.
        Box(modifier = Modifier.size(18.dp)) {
            if (selected) {
                Icon(
                    imageVector = Icons.Rounded.Check,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.primary,
                )
            }
        }
    }
}

/** An independent on/off, with the reason for it under the name. The whole row is the switch. */
@Composable
fun SwitchRow(
    title: String,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
    hint: String? = null,
    enabled: Boolean = true,
    modifier: Modifier = Modifier,
) {
    var focused by remember { mutableStateOf(false) }

    Row(
        modifier = modifier
            .fillMaxWidth()
            .cardSurface(focused = focused)
            .onFocusChanged { focused = it.isFocused }
            .toggleable(value = checked, enabled = enabled, role = Role.Switch, onValueChange = onCheckedChange)
            .heightIn(min = 48.dp)
            .padding(start = 14.dp, end = 10.dp, top = 10.dp, bottom = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        RowText(title = title, hint = hint, modifier = Modifier.weight(1f).alpha(if (enabled) 1f else 0.6f))
        Switch(
            checked = checked,
            onCheckedChange = null,
            enabled = enabled,
            colors = SwitchDefaults.colors(
                checkedThumbColor = MaterialTheme.colorScheme.onPrimary,
                checkedTrackColor = MaterialTheme.colorScheme.primary,
                uncheckedThumbColor = MaterialTheme.colorScheme.onSurfaceVariant,
                uncheckedTrackColor = MaterialTheme.colorScheme.surfaceVariant,
                uncheckedBorderColor = MaterialTheme.colorScheme.outline,
            ),
        )
    }
}

// Outlives the page, as an iOS Task does: leaving mid-save must not cancel the save.
private val saveScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

private class PendingSwitch(val value: Boolean)

/** A SwitchRow over a stored value (iOS StoreSwitchRow): it shows the tapped value at once while
 *  save runs, and only the newest tap clears that pending state. save reports its own failure. */
@Composable
fun StoreSwitchRow(
    title: String,
    value: Boolean,
    save: suspend (Boolean) -> Unit,
    hint: String? = null,
    enabled: Boolean = true,
    modifier: Modifier = Modifier,
) {
    var pending by remember { mutableStateOf<PendingSwitch?>(null) }

    SwitchRow(
        title = title,
        checked = pending?.value ?: value,
        onCheckedChange = { next ->
            val tap = PendingSwitch(next)
            pending = tap
            saveScope.launch {
                runCatching { save(next) }
                if (pending === tap) pending = null
            }
        },
        hint = hint,
        enabled = enabled,
        modifier = modifier,
    )
}

/** The token field's card, for the short values settings collects. label is what TalkBack reads. */
@Composable
fun SettingsField(
    value: String,
    onValueChange: (String) -> Unit,
    placeholder: String,
    label: String? = null,
    enabled: Boolean = true,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
    keyboardActions: KeyboardActions = KeyboardActions.Default,
    modifier: Modifier = Modifier,
) {
    var focused by remember { mutableStateOf(false) }

    val textStyle = TextStyle(
        color = MaterialTheme.colorScheme.onSurface,
        fontSize = 15.sp,
    )

    BasicTextField(
        value = value,
        onValueChange = onValueChange,
        modifier = modifier
            .fillMaxWidth()
            .cardSurface(focused = focused)
            .onFocusChanged { focused = it.isFocused }
            .then(if (label == null) Modifier else Modifier.semantics { contentDescription = label }),
        enabled = enabled,
        textStyle = textStyle,
        singleLine = true,
        cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
        keyboardOptions = keyboardOptions,
        keyboardActions = keyboardActions,
        decorationBox = { field ->
            Box(
                modifier = Modifier
                    .heightIn(min = 48.dp)
                    .padding(horizontal = 14.dp, vertical = 13.dp),
                contentAlignment = Alignment.CenterStart,
            ) {
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

/** A text setting applied only on Save or the keyboard's Done; Save shows while the text differs
 *  from initial, and a new initial resets it. label is for TalkBack: show it as SettingsGroup's title. */
@Composable
fun CommittedField(
    label: String,
    initial: String,
    onCommit: (String) -> Unit,
    placeholder: String = "",
    hint: String? = null,
    error: String? = null,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
    transform: (String) -> String = { it },
    modifier: Modifier = Modifier,
) {
    var text by remember(initial) { mutableStateOf(initial) }
    val dirty = text != initial
    val focusManager = LocalFocusManager.current

    Column(
        modifier = modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(9.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            SettingsField(
                value = text,
                onValueChange = { text = transform(it) },
                placeholder = placeholder,
                label = label,
                keyboardOptions = keyboardOptions.copy(imeAction = ImeAction.Done),
                keyboardActions = KeyboardActions(
                    onDone = {
                        if (dirty) onCommit(text)
                        focusManager.clearFocus()
                    },
                ),
                modifier = Modifier.weight(1f),
            )
            if (dirty) {
                Box(
                    modifier = Modifier
                        .padding(start = 4.dp)
                        .heightIn(min = 48.dp)
                        .clip(PillShape)
                        .clickable(role = Role.Button) { onCommit(text) }
                        .padding(horizontal = 12.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        text = stringResource(R.string.settings_save),
                        color = MaterialTheme.colorScheme.primary,
                        fontSize = 13.sp,
                        fontWeight = FontWeight.SemiBold,
                        maxLines = 1,
                    )
                }
            }
        }
        if (!hint.isNullOrEmpty()) {
            Text(
                text = hint,
                modifier = Modifier.padding(horizontal = 4.dp),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontSize = 12.sp,
                lineHeight = 16.sp,
            )
        }
        if (!error.isNullOrEmpty()) {
            InfoNote(text = error, tone = NoteTone.ERROR)
        }
    }
}

/** Keeps only the digits of a numeric field, folding Persian and Arabic-Indic ones to ASCII. */
fun asciiDigits(text: String): String = buildString {
    for (char in text) {
        val digit = Character.digit(char, 10)
        if (digit >= 0) append('0' + digit)
    }
}

/** A name and its monospaced value, 48 dp tall. Stack several in a PangeaCard with zero padding. */
@Composable
fun KeyValueRow(label: String, value: String, modifier: Modifier = Modifier) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .heightIn(min = 48.dp)
            .padding(horizontal = 14.dp, vertical = 8.dp)
            .semantics(mergeDescendants = true) {},
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            text = label,
            color = MaterialTheme.colorScheme.onSurface,
            fontSize = 15.sp,
            fontWeight = FontWeight.SemiBold,
        )
        Text(
            text = value,
            modifier = Modifier.weight(1f),
            color = LocalTextSecondary.current,
            fontFamily = FontFamily.Monospace,
            fontSize = 14.sp,
            textAlign = TextAlign.End,
        )
    }
}
