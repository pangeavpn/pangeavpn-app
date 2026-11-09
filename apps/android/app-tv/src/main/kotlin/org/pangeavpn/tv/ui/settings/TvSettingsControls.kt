package org.pangeavpn.tv.ui.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
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
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.rounded.Check
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.disabled
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.InfoNote
import org.pangeavpn.ui.components.NoteTone
import org.pangeavpn.ui.components.PangeaDialog
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.theme.LocalTextSecondary

internal val TvRowShape = RoundedCornerShape(12.dp)

// Outlives the section: moving along the rail must not cancel a save or drop its toast.
internal val settingsScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

/** A save's toast: the success text, or the generic failure. */
internal fun report(saved: Boolean, success: String, failed: String, style: ToastStyle = ToastStyle.SUCCESS) {
    if (saved) ToastCenter.show(success, style) else ToastCenter.show(failed, ToastStyle.WARNING)
}

@Composable
internal fun ltrStyle(): TextStyle = LocalTextStyle.current.copy(textDirection = TextDirection.Ltr)

/** The card every focusable TV row shares: a washed accent fill and a thick ring while focused. */
@Composable
internal fun Modifier.tvSurface(focused: Boolean, selected: Boolean = false): Modifier {
    val colors = MaterialTheme.colorScheme
    return clip(TvRowShape)
        .background(
            when {
                focused -> colors.primary.copy(alpha = 0.22f)
                selected -> colors.primary.copy(alpha = 0.1f)
                else -> colors.surface
            },
        )
        .border(
            width = if (focused) 2.dp else 1.dp,
            color = if (focused) colors.primary else colors.outlineVariant,
            shape = TvRowShape,
        )
}

/** The plain card for content that takes no focus. */
@Composable
internal fun Modifier.tvCard(): Modifier =
    clip(TvRowShape)
        .background(MaterialTheme.colorScheme.surface)
        .border(1.dp, MaterialTheme.colorScheme.outlineVariant, TvRowShape)

/** A card of facts the remote can still land on, so a page without controls scrolls too. */
@Composable
internal fun TvInfoCard(modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    var focused by remember { mutableStateOf(false) }

    Column(
        modifier = modifier
            .fillMaxWidth()
            .tvSurface(focused)
            .onFocusChanged { focused = it.isFocused }
            .focusable()
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
        content = content,
    )
}

@Composable
internal fun TvGroupLabel(text: String, modifier: Modifier = Modifier) {
    Text(
        text = text.uppercase(),
        modifier = modifier.padding(start = 4.dp, top = 8.dp).semantics { heading() },
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        fontSize = 12.sp,
        fontWeight = FontWeight.Bold,
        letterSpacing = 1.4.sp,
    )
}

@Composable
internal fun TvParagraph(text: String, modifier: Modifier = Modifier) {
    Text(
        text = text,
        modifier = modifier.padding(horizontal = 4.dp),
        color = LocalTextSecondary.current,
        fontSize = 14.sp,
        lineHeight = 20.sp,
    )
}

@Composable
private fun RowText(
    title: String,
    hint: String?,
    note: String?,
    badge: (@Composable () -> Unit)?,
    modifier: Modifier,
) {
    Column(modifier = modifier, verticalArrangement = Arrangement.spacedBy(3.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = title,
                modifier = Modifier.weight(1f, fill = false),
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 16.sp,
                fontWeight = FontWeight.SemiBold,
            )
            badge?.invoke()
        }
        if (!hint.isNullOrEmpty()) {
            Text(
                text = hint,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontSize = 13.sp,
                lineHeight = 18.sp,
            )
        }
        if (!note.isNullOrEmpty()) {
            Text(
                text = note,
                color = LocalTextSecondary.current,
                fontSize = 13.sp,
                lineHeight = 18.sp,
                fontWeight = FontWeight.Medium,
            )
        }
    }
}

/** An on/off setting the centre button flips, its state spelled out. A disabled row keeps
 *  its focus so the remote can still reach it and read why. */
@Composable
internal fun TvToggleRow(
    title: String,
    checked: Boolean,
    onToggle: (Boolean) -> Unit,
    modifier: Modifier = Modifier,
    hint: String? = null,
    note: String? = null,
    enabled: Boolean = true,
    badge: (@Composable () -> Unit)? = null,
) {
    var focused by remember { mutableStateOf(false) }

    Row(
        modifier = modifier
            .fillMaxWidth()
            .tvSurface(focused)
            .onFocusChanged { focused = it.isFocused }
            .toggleable(value = checked, role = Role.Switch) { if (enabled) onToggle(it) }
            .semantics { if (!enabled) disabled() }
            .heightIn(min = 56.dp)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        RowText(
            title = title,
            hint = hint,
            note = note,
            badge = badge,
            modifier = Modifier.weight(1f).alpha(if (enabled) 1f else 0.55f),
        )
        Text(
            text = stringResource(if (checked) R.string.toggle_on else R.string.toggle_off),
            color = if (checked && enabled) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
            fontSize = 15.sp,
            fontWeight = FontWeight.SemiBold,
        )
    }
}

private class PendingTap(val value: Boolean)

/** A TvToggleRow over a stored value, as the phone's StoreSwitchRow: the press shows at once
 *  while save runs, and only the newest press clears it. save reports its own failure. */
@Composable
internal fun TvStoreToggleRow(
    title: String,
    value: Boolean,
    save: suspend (Boolean) -> Unit,
    modifier: Modifier = Modifier,
    hint: String? = null,
    note: String? = null,
    enabled: Boolean = true,
) {
    var pending by remember { mutableStateOf<PendingTap?>(null) }

    TvToggleRow(
        title = title,
        checked = pending?.value ?: value,
        onToggle = { next ->
            val tap = PendingTap(next)
            pending = tap
            settingsScope.launch {
                runCatching { save(next) }
                if (pending === tap) pending = null
            }
        },
        modifier = modifier,
        hint = hint,
        note = note,
        enabled = enabled,
    )
}

/** One option of a set, ticked when chosen. The parent carries Modifier.selectableGroup(). */
@Composable
internal fun TvChoiceRow(
    label: String,
    selected: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    hint: String? = null,
    enabled: Boolean = true,
) {
    var focused by remember { mutableStateOf(false) }

    Row(
        modifier = modifier
            .fillMaxWidth()
            .tvSurface(focused, selected)
            .onFocusChanged { focused = it.isFocused }
            .selectable(selected = selected, role = Role.RadioButton) { if (enabled) onClick() }
            .semantics { if (!enabled) disabled() }
            .heightIn(min = 52.dp)
            .padding(horizontal = 16.dp, vertical = 11.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        RowText(
            title = label,
            hint = hint,
            note = null,
            badge = null,
            modifier = Modifier.weight(1f).alpha(if (enabled) 1f else 0.55f),
        )
        Box(modifier = Modifier.size(22.dp)) {
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

/** A row that does one thing, such as Sign Out; loading dims it and adds a spinner. */
@Composable
internal fun TvActionRow(
    icon: ImageVector,
    title: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    tint: Color? = null,
    loading: Boolean = false,
    enabled: Boolean = true,
) {
    var focused by remember { mutableStateOf(false) }
    val color = tint ?: MaterialTheme.colorScheme.onSurface
    val live = enabled && !loading

    Row(
        modifier = modifier
            .fillMaxWidth()
            .tvSurface(focused)
            .onFocusChanged { focused = it.isFocused }
            .clickable(role = Role.Button) { if (live) onClick() }
            .semantics { if (!enabled) disabled() }
            .heightIn(min = 56.dp)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            modifier = Modifier.size(22.dp).alpha(if (live) 1f else 0.5f),
            tint = color,
        )
        Text(
            text = title,
            modifier = Modifier.weight(1f).alpha(if (live) 1f else 0.5f),
            color = color,
            fontSize = 16.sp,
            fontWeight = FontWeight.SemiBold,
        )
        if (loading) {
            CircularProgressIndicator(modifier = Modifier.size(20.dp), color = color, strokeWidth = 2.dp)
        }
    }
}

/** A text setting as a row: its value, and the centre button opens the editor. muted shows
 *  value as a stand-in (nothing set yet) rather than as data. */
@Composable
internal fun TvFieldRow(
    label: String,
    value: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    hint: String? = null,
    muted: Boolean = false,
    ltr: Boolean = true,
) {
    var focused by remember { mutableStateOf(false) }

    Row(
        modifier = modifier
            .fillMaxWidth()
            .tvSurface(focused)
            .onFocusChanged { focused = it.isFocused }
            .clickable(role = Role.Button, onClick = onClick)
            .heightIn(min = 56.dp)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(
                text = label,
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 16.sp,
                fontWeight = FontWeight.SemiBold,
            )
            Text(
                text = value,
                style = if (ltr && !muted) ltrStyle() else LocalTextStyle.current,
                color = if (muted) MaterialTheme.colorScheme.onSurfaceVariant else LocalTextSecondary.current,
                fontFamily = if (muted) FontFamily.Default else FontFamily.Monospace,
                fontSize = 14.sp,
                lineHeight = 20.sp,
            )
            if (!hint.isNullOrEmpty()) {
                Text(
                    text = hint,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    fontSize = 13.sp,
                    lineHeight = 18.sp,
                )
            }
        }
        Icon(
            imageVector = Icons.Outlined.Edit,
            contentDescription = null,
            modifier = Modifier.size(20.dp),
            tint = if (focused) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

/** The editor behind a TvFieldRow. The field takes focus on opening, so the system keyboard
 *  comes up at once; Done or Save hands the text to onSave, which closes it or sets error. */
@Composable
internal fun TvEditDialog(
    title: String,
    initial: String,
    onSave: (String) -> Unit,
    onDismiss: () -> Unit,
    placeholder: String = "",
    hint: String? = null,
    error: String? = null,
    keyboardType: KeyboardType = KeyboardType.Text,
    transform: (String) -> String = { it },
    counter: ((String) -> String)? = null,
    ltr: Boolean = true,
) {
    var field by remember { mutableStateOf(TextFieldValue(initial, TextRange(initial.length))) }
    var focused by remember { mutableStateOf(false) }
    val focus = remember { FocusRequester() }
    val textStyle = TextStyle(
        color = MaterialTheme.colorScheme.onSurface,
        fontSize = 17.sp,
        textDirection = if (ltr) TextDirection.Ltr else TextDirection.Content,
    )

    PangeaDialog(title = title, onDismiss = onDismiss) {
        BasicTextField(
            value = field,
            onValueChange = { next ->
                val shaped = transform(next.text)
                field = if (shaped == next.text) next else TextFieldValue(shaped, TextRange(shaped.length))
            },
            modifier = Modifier
                .fillMaxWidth()
                .tvSurface(focused)
                .onFocusChanged { focused = it.isFocused }
                .focusRequester(focus),
            textStyle = textStyle,
            singleLine = true,
            cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
            keyboardOptions = KeyboardOptions(keyboardType = keyboardType, imeAction = ImeAction.Done),
            keyboardActions = KeyboardActions(onDone = { onSave(field.text) }),
            decorationBox = { inner ->
                Box(
                    modifier = Modifier
                        .heightIn(min = 52.dp)
                        .padding(horizontal = 16.dp, vertical = 14.dp),
                    contentAlignment = Alignment.CenterStart,
                ) {
                    if (field.text.isEmpty()) {
                        Text(text = placeholder, style = textStyle.copy(color = MaterialTheme.colorScheme.onSurfaceVariant))
                    }
                    inner()
                }
            },
        )
        if (counter != null) {
            Text(
                text = counter(field.text),
                modifier = Modifier.fillMaxWidth(),
                style = ltrStyle(),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontFamily = FontFamily.Monospace,
                fontSize = 12.sp,
                textAlign = TextAlign.End,
            )
        }
        if (!hint.isNullOrEmpty()) {
            Text(
                text = hint,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontSize = 13.sp,
                lineHeight = 18.sp,
            )
        }
        if (!error.isNullOrEmpty()) {
            InfoNote(text = error, tone = NoteTone.ERROR)
        }
        PillButton(label = stringResource(R.string.settings_save), onClick = { onSave(field.text) })
        GhostButton(label = stringResource(R.string.settings_home_cancel), onClick = onDismiss)
        LaunchedEffect(Unit) { focus.requestFocus() }
    }
}

/** ConfirmDialog with remote focus placed for it: on Cancel when the action is destructive. */
@Composable
internal fun TvConfirmDialog(
    title: String,
    message: String,
    confirmLabel: String,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
    destructive: Boolean = false,
    cancelLabel: String = stringResource(R.string.settings_home_cancel),
) {
    val initial = remember { FocusRequester() }

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
            modifier = if (destructive) Modifier else Modifier.focusRequester(initial),
        )
        GhostButton(
            label = cancelLabel,
            onClick = onDismiss,
            modifier = if (destructive) Modifier.focusRequester(initial) else Modifier,
        )
        LaunchedEffect(Unit) { initial.requestFocus() }
    }
}
