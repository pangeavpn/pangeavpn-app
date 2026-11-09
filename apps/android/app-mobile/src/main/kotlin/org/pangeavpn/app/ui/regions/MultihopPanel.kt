package org.pangeavpn.app.ui.regions

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.AutoAwesome
import androidx.compose.material.icons.rounded.DeviceHub
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.key
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.core.servers.EntryChip
import org.pangeavpn.core.servers.EntryPanel
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.PangeaCard
import org.pangeavpn.ui.components.SectionLabel
import org.pangeavpn.ui.components.flagEmoji
import org.pangeavpn.ui.text.entryHintText
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.Motion
import org.pangeavpn.ui.theme.reducedMotion

private val GlyphShape = RoundedCornerShape(11.dp)

/** The multihop switch and, while it is on, the entry choice. The whole head row is the switch. */
@Composable
internal fun MultihopPanel(
    enabled: Boolean,
    panel: EntryPanel,
    onToggle: (Boolean) -> Unit,
    onChoose: (String?) -> Unit,
) {
    val reduced = reducedMotion()
    PangeaCard(padding = PaddingValues(0.dp)) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .toggleable(value = enabled, role = Role.Switch, onValueChange = onToggle)
                .padding(horizontal = 14.dp, vertical = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Box(
                modifier = Modifier
                    .size(36.dp)
                    .clip(GlyphShape)
                    .background(MaterialTheme.colorScheme.primary.copy(alpha = 0.15f)),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = Icons.Rounded.DeviceHub,
                    contentDescription = null,
                    modifier = Modifier.size(20.dp),
                    tint = MaterialTheme.colorScheme.primary,
                )
            }
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = stringResource(R.string.regions_multihop),
                    color = MaterialTheme.colorScheme.onSurface,
                    fontSize = 15.sp,
                    fontWeight = FontWeight.SemiBold,
                )
                Text(
                    text = stringResource(R.string.regions_multihop_hint),
                    modifier = Modifier.padding(top = 2.dp),
                    color = LocalTextSecondary.current,
                    fontSize = 12.sp,
                    lineHeight = 16.sp,
                )
            }
            Switch(
                checked = enabled,
                onCheckedChange = null,
                colors = SwitchDefaults.colors(
                    checkedThumbColor = MaterialTheme.colorScheme.onPrimary,
                    checkedTrackColor = MaterialTheme.colorScheme.primary,
                    uncheckedThumbColor = MaterialTheme.colorScheme.onSurfaceVariant,
                    uncheckedTrackColor = MaterialTheme.colorScheme.surfaceVariant,
                    uncheckedBorderColor = MaterialTheme.colorScheme.outline,
                ),
            )
        }
        AnimatedVisibility(
            visible = enabled,
            enter = fadeIn(animationSpec = Motion.quick()) + expandVertically(animationSpec = Motion.layout(reduced)),
            exit = fadeOut(animationSpec = Motion.quick()) + shrinkVertically(animationSpec = Motion.layout(reduced)),
        ) {
            EntryChooser(panel = panel, onChoose = onChoose)
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun EntryChooser(panel: EntryPanel, onChoose: (String?) -> Unit) {
    Column(modifier = Modifier.fillMaxWidth().padding(start = 14.dp, end = 14.dp, bottom = 12.dp)) {
        HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
        SectionLabel(
            text = stringResource(R.string.regions_entry),
            modifier = Modifier.padding(top = 12.dp, bottom = 2.dp),
        )
        FlowRow(
            modifier = Modifier.selectableGroup(),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            panel.chips.forEach { chip ->
                key(chip.regionKey) {
                    EntryChipButton(chip = chip, onClick = { onChoose(chip.serverId) })
                }
            }
        }
        entryHintText(panel.hint)?.let { hint ->
            Text(
                text = hint,
                modifier = Modifier.padding(top = 2.dp),
                color = LocalTextSecondary.current,
                fontFamily = FontFamily.Monospace,
                fontSize = 11.sp,
                lineHeight = 15.sp,
            )
        }
    }
}

// Drawn at 36 dp; the minimum-size modifier pads the layout and the touch target out to 48.
@Composable
private fun EntryChipButton(chip: EntryChip, onClick: () -> Unit) {
    val auto = chip.regionKey == null
    val exitNote = stringResource(R.string.regions_same_as_exit)
    val colors = MaterialTheme.colorScheme
    Row(
        modifier = Modifier
            .minimumInteractiveComponentSize()
            .alpha(if (chip.isExit) 0.4f else 1f)
            .clip(CircleShape)
            .background(if (chip.selected) colors.primary.copy(alpha = 0.15f) else colors.surfaceVariant)
            .border(width = 1.dp, color = if (chip.selected) colors.primary else colors.outline, shape = CircleShape)
            .selectable(selected = chip.selected, enabled = !chip.isExit, role = Role.RadioButton, onClick = onClick)
            .then(if (chip.isExit) Modifier.semantics { stateDescription = exitNote } else Modifier)
            .heightIn(min = 36.dp)
            .padding(start = 10.dp, end = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(7.dp),
    ) {
        if (auto) {
            Icon(
                imageVector = Icons.Rounded.AutoAwesome,
                contentDescription = null,
                modifier = Modifier.size(14.dp),
                tint = colors.primary,
            )
        } else {
            Text(
                text = flagEmoji(chip.country),
                modifier = Modifier.clearAndSetSemantics {},
                fontSize = 15.sp,
            )
        }
        Text(
            text = if (auto) stringResource(R.string.regions_entry_auto) else chip.name,
            color = if (chip.selected) colors.onSurface else LocalTextSecondary.current,
            fontSize = 13.sp,
            fontWeight = FontWeight.SemiBold,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}
