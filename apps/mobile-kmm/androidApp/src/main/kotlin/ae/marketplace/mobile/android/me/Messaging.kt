package ae.marketplace.mobile.android.me

import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.me.MeViewModel
import ae.marketplace.mobile.model.Conversation
import ae.marketplace.mobile.model.Message
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * The inbox.
 *
 * This is the other half of the CTA. "Message seller" has been on every reel in
 * the feed since Phase 3 and until now it recorded a click and went nowhere —
 * so the conversion the whole video bet is measured on had no destination in
 * the app at all.
 */
@Composable
fun InboxScreen(
    viewModel: MeViewModel,
    onOpen: (Conversation) -> Unit,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val state by viewModel.inbox.collectAsState()
    LaunchedEffect(Unit) { viewModel.loadInbox() }

    Column(modifier.fillMaxSize().background(Ink).statusBarsPadding()) {
        Header(stringResource(R.string.me_messages), onBack)
        when {
            state.loading && state.items.isEmpty() -> Centered { CircularProgressIndicator(color = Accent) }
            state.error != null -> Hint(state.error ?: "", bad = true)
            state.items.isEmpty() -> Hint(stringResource(R.string.inbox_empty))
            else -> LazyColumn {
                items(state.items, key = { it.id }) { c -> InboxRow(c) { onOpen(c) } }
            }
        }
    }
}

@Composable
private fun InboxRow(c: Conversation, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 18.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(
                text = c.listingTitle.ifEmpty { stringResource(R.string.inbox_untitled) },
                color = Color.White,
                fontSize = 15.sp,
                fontWeight = if (c.unread > 0) FontWeight.SemiBold else FontWeight.Normal,
            )
            if (c.state != "open" && c.state.isNotEmpty()) {
                // A closed or blocked thread is stated. A buyer typing into a
                // thread the seller closed and getting silence is worse than
                // being told.
                Text(
                    text = c.state,
                    color = Color.White.copy(alpha = 0.35f),
                    fontSize = 11.sp,
                    modifier = Modifier.padding(top = 2.dp),
                )
            }
        }
        if (c.unread > 0) {
            Box(
                Modifier.size(20.dp).background(Accent, RoundedCornerShape(50)),
                contentAlignment = Alignment.Center,
            ) {
                Text("${c.unread}", color = Color.White, fontSize = 10.sp, fontWeight = FontWeight.Bold)
            }
        }
    }
}

/**
 * One conversation.
 *
 * Two things here are not cosmetic.
 *
 * There is no optimistic bubble — the message that appears is the one the
 * server returned. A message can come back `held`, meaning a moderator has it
 * and it has *not* been delivered, or `redacted`, meaning a phone number was
 * stripped out of it. Drawing what the sender typed and moving on would
 * misreport both, and a sender who thinks a held message arrived concludes they
 * are being ignored and goes looking for a way off the platform — which is
 * precisely what the redaction exists to prevent.
 *
 * And the draft survives a failure. Losing what someone typed because the
 * network dropped is the most annoying failure a messaging screen has.
 */
@Composable
fun ThreadScreen(viewModel: MeViewModel, onBack: () -> Unit, modifier: Modifier = Modifier) {
    val state by viewModel.thread.collectAsState()
    val listState = rememberLazyListState()

    LaunchedEffect(state.messages.size) {
        if (state.messages.isNotEmpty()) listState.animateScrollToItem(state.messages.lastIndex)
    }

    Column(modifier.fillMaxSize().background(Ink).statusBarsPadding().imePadding()) {
        Header(state.title.ifEmpty { stringResource(R.string.me_messages) }, onBack)

        Box(Modifier.weight(1f)) {
            when {
                state.loading -> Centered { CircularProgressIndicator(color = Accent) }
                state.messages.isEmpty() -> Hint(stringResource(R.string.thread_empty))
                else -> LazyColumn(
                    state = listState,
                    contentPadding = androidx.compose.foundation.layout.PaddingValues(vertical = 8.dp),
                ) {
                    items(state.messages, key = { it.id }) { Bubble(it) }
                }
            }
        }

        state.error?.let {
            Text(it, color = Accent, fontSize = 12.sp, modifier = Modifier.padding(horizontal = 18.dp, vertical = 4.dp))
        }

        Row(
            Modifier
                .navigationBarsPadding()
                .fillMaxWidth()
                .padding(horizontal = 12.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Box(
                Modifier
                    .weight(1f)
                    .border(1.dp, Line, RoundedCornerShape(20.dp))
                    .padding(horizontal = 14.dp, vertical = 11.dp),
            ) {
                if (state.draft.isEmpty()) {
                    Text(
                        stringResource(R.string.thread_hint),
                        color = Color.White.copy(alpha = 0.28f),
                        fontSize = 14.sp,
                    )
                }
                BasicTextField(
                    value = state.draft,
                    onValueChange = viewModel::onDraftChanged,
                    textStyle = TextStyle(color = Color.White, fontSize = 14.sp),
                    cursorBrush = SolidColor(Accent),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            val canSend = state.draft.isNotBlank() && !state.sending
            Box(
                Modifier
                    .background(if (canSend) Accent else Accent.copy(alpha = 0.3f), RoundedCornerShape(20.dp))
                    .clickable(enabled = canSend) { viewModel.sendDraft() }
                    .padding(horizontal = 16.dp, vertical = 11.dp),
            ) {
                Text(stringResource(R.string.thread_send), color = Color.White, fontSize = 14.sp, fontWeight = FontWeight.Medium)
            }
        }
    }
}

@Composable
private fun Bubble(m: Message) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 14.dp, vertical = 4.dp)) {
        Box(
            Modifier
                .widthIn(max = 300.dp)
                .background(Color(0xFF1C1C20), RoundedCornerShape(14.dp))
                .padding(horizontal = 13.dp, vertical = 9.dp),
        ) {
            Text(m.body, color = Color.White.copy(alpha = if (m.state == "held") 0.5f else 0.92f), fontSize = 14.sp)
        }
        // The state, in the sender's own words, under the bubble it belongs to.
        stateNote(m)?.let {
            Text(it, color = Accent.copy(alpha = 0.85f), fontSize = 10.sp, modifier = Modifier.padding(top = 3.dp))
        }
    }
}

@Composable
private fun stateNote(m: Message): String? = when (m.state) {
    "held" -> stringResource(R.string.msg_held)
    "redacted" -> stringResource(R.string.msg_redacted)
    "blocked" -> stringResource(R.string.msg_blocked)
    else -> null
}

@Composable
private fun Header(title: String, onBack: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 14.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            "‹",
            color = Color.White,
            fontSize = 26.sp,
            modifier = Modifier.clickable(onClick = onBack).padding(end = 12.dp),
        )
        Text(title, color = Color.White, fontSize = 17.sp, fontWeight = FontWeight.SemiBold)
    }
}
