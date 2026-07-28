package ae.marketplace.mobile.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * The client engagement events the ranker trains on.
 *
 * Mirrors `#/components/schemas/ClientEvent`. Two fields deserve care because
 * getting them wrong corrupts ranking rather than breaking a screen.
 *
 * [watchedMs] is sent raw and the **server** applies the ≥2000 ms view
 * definition. The API says so explicitly, and the reason is that Phase 4's
 * ranking model trains on the same definition — two implementations of the word
 * "view" is how a model learns the wrong thing.
 *
 * [completed] is reported separately from [watchedMs] rather than derived from
 * it. "Did they watch it" and "did they finish it" answer different questions
 * and the ranker weights them differently; a client that sets `completed` when
 * `watchedMs >= durationMs` looks equivalent and silently drops every reel a
 * viewer replayed.
 */
@Serializable
data class ClientEvent(
    val type: String,
    @SerialName("reel_id") val reelId: String? = null,
    /** Client clock. Kept alongside server receipt time so an offline backlog is visible. */
    @SerialName("occurred_at") val occurredAt: String,
    @SerialName("watched_ms") val watchedMs: Int? = null,
    val completed: Boolean = false,
    val properties: Map<String, String> = emptyMap(),
) {
    companion object {
        const val VIEW_START = "view_start"
        const val VIEW_PROGRESS = "view_progress"
        const val SKIP = "skip"
        const val LIKE = "like"
        const val UNLIKE = "unlike"
        const val SHARE = "share"
        const val SAVE = "save"
        const val UNSAVE = "unsave"
        const val ACTION_CLICK = "action_click"
    }
}

@Serializable
data class EventBatch(
    @SerialName("session_id") val sessionId: String,
    val events: List<ClientEvent>,
)
