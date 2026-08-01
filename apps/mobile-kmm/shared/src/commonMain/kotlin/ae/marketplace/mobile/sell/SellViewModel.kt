package ae.marketplace.mobile.sell

import ae.marketplace.mobile.network.Failure
import ae.marketplace.mobile.network.toFailure
import ae.marketplace.mobile.model.ClockOption
import ae.marketplace.mobile.model.SellForm
import ae.marketplace.mobile.model.SellOption
import ae.marketplace.mobile.model.SellStatus
import ae.marketplace.mobile.model.SellVertical
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

/**
 * Where the seller is.
 *
 * Camera first and form last, which is the whole shape of the flow: a seller who
 * has already filmed a car has sunk thirty seconds into it and finishes the
 * form; a seller shown a form first closes the app. The vertical is chosen
 * *after* filming for the same reason — it is a question about the video that
 * already exists rather than a gate in front of the camera.
 */
enum class SellStep {
    Record,
    /** "What are you listing?" — the tile grid, served. */
    Vertical,
    /** The vertical's fields, served. */
    Details,
    /** The clock and the place. */
    Clock,
    /** Uploading, processing, and then whatever the pipeline says. */
    Live,
}

/**
 * How far the video has got.
 *
 * Separate from [SellStep] because they advance independently: the seller
 * reaches Live the moment they tap Publish, and the bytes are still going up
 * behind that screen. Merging them would either block the seller on the upload
 * or lie about it.
 */
enum class UploadPhase {
    Idle,
    Uploading,
    Processing,
    Done,
    /** The bytes did not get there. Retrying the upload is what fixes it. */
    Failed,
    /**
     * The bytes arrived but the server refused to publish — a quota, a rule, a
     * conflict.
     *
     * Distinct from [Failed] because the two need different words and different
     * buttons, which the first version of this got wrong: it reported a refused
     * publish as "the upload did not finish" over a video that had uploaded
     * perfectly, and offered to retry the one thing that had worked.
     */
    Refused,
}

data class SellUiState(
    val step: SellStep = SellStep.Record,

    /** Set by the platform recorder when a clip is captured. */
    val clipDurationMs: Long = 0,
    val hasClip: Boolean = false,

    val verticals: List<SellVertical> = emptyList(),
    /**
     * Whether the tile grid has been answered for, either way.
     *
     * Needed because an empty [verticals] means three different things and the
     * Vertical step rendered all three as the third: still loading, the request
     * failed, and this deployment genuinely has nothing sellable. It showed
     * "Nothing is sellable right now" for the whole of the first fetch and for
     * every failure — a false statement about a working product, made to
     * someone who has just recorded a video.
     */
    val verticalsLoaded: Boolean = false,
    val selected: SellVertical? = null,
    val form: SellForm? = null,

    /** Field key → the seller's answer. Posted verbatim. */
    val values: Map<String, String> = emptyMap(),
    /**
     * Field key → what the seller should *see* for that answer.
     *
     * Only lookups populate it. The value posted for a car is an opaque token
     * the vertical minted — "nissan/patrol/2021" — and showing that in the box
     * would be showing someone a database key. Two maps rather than parsing the
     * token, because the format belongs to the vertical and this class must
     * never learn it.
     */
    val lookupLabels: Map<String, String> = emptyMap(),
    val issues: List<FieldIssue> = emptyList(),

    /** Rows the lookup field last returned, keyed by the field it was for. */
    val lookupFor: String = "",
    val lookupResults: List<SellOption> = emptyList(),
    val lookupBusy: Boolean = false,

    val clock: String = "",
    val region: String = "",
    val place: String = "",
    val lat: Double = 0.0,
    val lng: Double = 0.0,

    val busy: Boolean = false,
    /**
     * Why the last step failed, classified.
     *
     * A [Failure], not the `e.message` this held while every other screen had
     * already moved on. The sell flow was the last one left, and it is the
     * worst place to leave it: by the time this field is set the seller has
     * recorded a video, filled a form and waited on an upload, and what they
     * were shown for their trouble was a raw exception message — untranslatable
     * in an app that ships Arabic, and frequently just a class name.
     *
     * The nullability was the sharper edge. `e.message` is `String?`, and this
     * field's word for *nothing went wrong* is also null, so a throwable that
     * carried no message rendered a failed publish as a silent success: no
     * error, no retry, and a reel the seller believed was live.
     */
    val error: Failure? = null,

    val reelId: String? = null,
    val reviewNote: String = "",
    val upload: UploadPhase = UploadPhase.Idle,
    val status: SellStatus? = null,

    // What the seller is selling, kept after the draft is created so the trust
    // document has something to attach to. A document cannot be uploaded before
    // this exists — there is no subject to attach it to — which is why the
    // affordance lives on the Live screen and not on the form.
    val subjectId: String? = null,
    val subjectType: String = "",
    val trust: TrustState = TrustState(),
) {
    /** The chosen clock, or the default the server marked. */
    val activeClock: ClockOption?
        get() = form?.clocks?.firstOrNull { it.key == clock }
            ?: form?.clocks?.firstOrNull { it.default }
            ?: form?.clocks?.firstOrNull()

    /**
     * A single clock is a statement, not a choice.
     *
     * A control with one option cannot change anything, and drawing it as a
     * chooser implies the others exist — which for cars today they do not. Same
     * reasoning as the filter row, which is not drawn at all when the server
     * offers nothing to filter by.
     */
    val clockIsAChoice: Boolean get() = (form?.clocks?.size ?: 0) > 1

    val canSubmitDetails: Boolean
        get() = form?.let { SellValidation.isComplete(it, values) } == true

    val canPublish: Boolean get() = hasClip && place.isNotEmpty() && !busy
}

/**
 * The sell flow's state, shared.
 *
 * Everything here is platform-neutral, including the parts that look like they
 * should not be: the step machine, the field answers, the validation and the
 * upload phase. What is *not* here is the camera and the file — both platforms
 * hand this class a `ByteArray` and a duration, because a capture session is the
 * one part of this flow that is genuinely different on each OS.
 */
class SellViewModel(private val api: SellApi) : ViewModel() {

    private val _state = MutableStateFlow(SellUiState())
    val state: StateFlow<SellUiState> = _state.asStateFlow()

    /** The recorded clip, held until the draft exists to upload it to. */
    private var clip: ByteArray? = null
    private var lookupJob: Job? = null
    private var pollJob: Job? = null

    // The presign, kept so a retry can reuse it. Not in SellUiState: it is a
    // signed URL and state is what the UI reads, logs and sometimes dumps into a
    // crash report.
    private var pendingUploadUrl: String? = null
    private var pendingUploadHeaders: Map<String, String> = emptyMap()

    /**
     * Loads the tile grid.
     *
     * Called at flow start rather than when the recorder finishes, so the grid
     * is already in memory by the time the seller stops filming. It is two
     * hundred bytes and it removes a spinner from the middle of the flow.
     */
    fun start() {
        // Re-entrant: the Vertical step calls this again as its retry, and both
        // platforms call it from a lifecycle hook that can fire twice.
        if (_state.value.verticals.isNotEmpty()) return
        _state.value = _state.value.copy(error = null)
        viewModelScope.launch {
            runCatching { api.verticals() }
                .onSuccess {
                    _state.value = _state.value.copy(
                        verticals = it.verticals, verticalsLoaded = true,
                    )
                }
                .onFailure { e ->
                    // verticalsLoaded stays false, which is what lets the step
                    // draw the failure instead of "nothing sellable".
                    _state.value = _state.value.copy(error = e.toFailure())
                }
        }
    }

    /** The platform recorder finished. */
    fun onClipRecorded(bytes: ByteArray, durationMs: Long) {
        clip = bytes
        _state.value = _state.value.copy(
            hasClip = true,
            clipDurationMs = durationMs,
            step = SellStep.Vertical,
        )
    }

    fun onVerticalPicked(vertical: SellVertical) {
        _state.value = _state.value.copy(
            selected = vertical,
            // Cleared rather than kept. Two verticals can share a field key and
            // carrying "price_minor" from a car into a flat would prefill a
            // number the seller never typed for this listing.
            values = emptyMap(),
            issues = emptyList(),
            form = null,
            busy = true,
            error = null,
        )
        viewModelScope.launch {
            runCatching { api.form(vertical.key) }
                .onSuccess { form ->
                    _state.value = _state.value.copy(
                        form = form,
                        clock = form.clocks.firstOrNull { it.default }?.key.orEmpty(),
                        busy = false,
                        step = SellStep.Details,
                    )
                }
                .onFailure { e ->
                    // Back to the grid rather than forward into an empty form. A
                    // form step with no fields is a dead end with a Next button.
                    _state.value = _state.value.copy(
                        busy = false, step = SellStep.Vertical, error = e.toFailure(),
                    )
                }
        }
    }

    /**
     * A field changed.
     *
     * The issue for *this* field is dropped as the seller types, and no new one
     * is raised until they try to move on. Validating on every keystroke tells
     * someone their price is too low while they are still typing the first
     * digit, which is both true and useless.
     */
    fun onValueChanged(key: String, value: String) {
        _state.value = _state.value.copy(
            values = _state.value.values + (key to value),
            issues = _state.value.issues.filterNot { it.key == key },
        )
    }

    /**
     * The lookup field's typeahead.
     *
     * Debounced, and the previous request is cancelled rather than left to
     * finish. Without the cancel, a slow response for "pat" can land after the
     * fast one for "patrol" and replace a correct list with a stale one — which
     * on a picker reads as the app choosing for you.
     */
    fun onLookupQuery(field: String, query: String) {
        lookupJob?.cancel()
        if (query.isBlank()) {
            _state.value = _state.value.copy(lookupFor = field, lookupResults = emptyList())
            return
        }
        val source = _state.value.form?.fields?.firstOrNull { it.key == field }?.source ?: return

        lookupJob = viewModelScope.launch {
            delay(LOOKUP_DEBOUNCE_MS)
            _state.value = _state.value.copy(lookupFor = field, lookupBusy = true)
            runCatching { api.lookup(source, query) }
                .onSuccess {
                    _state.value = _state.value.copy(lookupResults = it.options, lookupBusy = false)
                }
                .onFailure {
                    // Silent. A typeahead that cannot reach the server should
                    // stop suggesting, not put an error over a form the seller is
                    // still filling in.
                    _state.value = _state.value.copy(lookupResults = emptyList(), lookupBusy = false)
                }
        }
    }

    fun onLookupPicked(field: String, option: SellOption) {
        _state.value = _state.value.copy(
            values = _state.value.values + (field to option.value),
            // The chosen row's label is kept beside the opaque value so the box
            // can show "2021 Nissan Patrol" while posting "nissan/patrol/2021".
            // Two keys rather than a parsed value: the format is the vertical's
            // and this class must never learn it.
            lookupLabels = _state.value.lookupLabels + (field to option.label),
            issues = _state.value.issues.filterNot { it.key == field },
            lookupResults = emptyList(),
            lookupFor = "",
        )
    }

    fun onDetailsSubmitted() {
        val form = _state.value.form ?: return
        val issues = SellValidation.check(form, _state.value.values)
        if (issues.isNotEmpty()) {
            _state.value = _state.value.copy(issues = issues)
            return
        }
        _state.value = _state.value.copy(issues = emptyList(), step = SellStep.Clock)
    }

    fun onClockPicked(key: String) {
        _state.value = _state.value.copy(clock = key)
    }

    fun onPlacePicked(region: String, place: String, lat: Double, lng: Double) {
        _state.value = _state.value.copy(region = region, place = place, lat = lat, lng = lng)
    }

    fun onBack() {
        val s = _state.value
        _state.value = when (s.step) {
            SellStep.Clock -> s.copy(step = SellStep.Details)
            SellStep.Details -> s.copy(step = SellStep.Vertical, form = null, selected = null)
            SellStep.Vertical -> s.copy(step = SellStep.Record, hasClip = false)
            else -> s
        }
    }

    /**
     * Publish.
     *
     * The seller reaches the live screen immediately and the upload runs behind
     * it. That is not a shortcut — it is the honest arrangement. The bytes take
     * as long as the network takes, the pipeline takes as long as transcoding and
     * a human reviewer take, and a spinner over the Publish button would hide all
     * of it behind one indeterminate wait with nothing to look at.
     *
     * The draft call happens here rather than after the form step, because the
     * clock and the place are part of it and both are chosen on the step before
     * this one. Creating the subject earlier to overlap the upload would mean
     * creating it with a listing term the seller had not picked yet.
     */
    fun onPublish() {
        val s = _state.value
        val vertical = s.selected ?: return
        val bytes = clip ?: return
        if (s.busy) return

        _state.value = s.copy(busy = true, error = null, step = SellStep.Live, upload = UploadPhase.Uploading)

        viewModelScope.launch {
            val draft = runCatching {
                api.createDraft(
                    SellDraftRequest(
                        subjectType = vertical.key,
                        values = s.values,
                        clock = s.activeClock?.key.orEmpty(),
                        lat = s.lat,
                        lng = s.lng,
                        region = s.region,
                        place = s.place,
                    )
                )
            }.getOrElse { e ->
                // Back to the form. A validation failure the client did not catch
                // names a field, and the seller cannot fix it from the live
                // screen.
                _state.value = _state.value.copy(
                    busy = false, step = SellStep.Details,
                    upload = UploadPhase.Idle, error = e.toFailure(),
                )
                return@launch
            }

            pendingUploadUrl = draft.uploadUrl
            pendingUploadHeaders = draft.uploadHeaders
            _state.value = _state.value.copy(
                reelId = draft.reelId,
                reviewNote = draft.reviewNote,
                // Kept so the Live screen can attach a trust document. Until
                // the draft exists there is no subject to attach one to, which
                // is why the affordance is there and not on the form.
                subjectId = draft.subjectId,
                subjectType = draft.subjectType,
            )

            runCatching { api.uploadVideo(draft.uploadUrl, draft.uploadHeaders, bytes) }
                .onFailure { e ->
                    // The listing row exists and the reel draft is waiting for
                    // bytes. Reported as a retryable upload failure rather than
                    // as a failed publish, because retrying the upload is exactly
                    // what fixes it — see [retryUpload].
                    _state.value = _state.value.copy(
                        busy = false, upload = UploadPhase.Failed, error = e.toFailure(),
                    )
                    return@launch
                }

            _state.value = _state.value.copy(upload = UploadPhase.Processing)

            runCatching { api.publish(draft.reelId) }
                .onSuccess {
                    _state.value = _state.value.copy(busy = false, status = it, upload = UploadPhase.Done)
                    watch(draft.reelId)
                }
                .onFailure { e ->
                    // Refused, not Failed. The video is in the bucket; what was
                    // rejected is the listing, and offering to retry the upload
                    // would send the seller to fix the one part that worked.
                    _state.value = _state.value.copy(
                        busy = false, upload = UploadPhase.Refused, error = e.toFailure(),
                    )
                }
        }
    }

    /**
     * Re-attempts the publish for a draft whose bytes are already up.
     *
     * The counterpart to [retryUpload], and separate from it because the two
     * repair different failures. This one re-runs only the server call: a quota
     * the seller has since resolved, a rule that has since changed, or — as this
     * flow's first run found — a credit that was granted a few milliseconds
     * after the check that looked for it.
     */
    fun retryPublish() {
        val reelId = _state.value.reelId ?: return
        if (_state.value.busy) return
        _state.value = _state.value.copy(busy = true, error = null, upload = UploadPhase.Processing)
        viewModelScope.launch {
            runCatching { api.publish(reelId) }
                .onSuccess {
                    _state.value = _state.value.copy(busy = false, status = it, upload = UploadPhase.Done)
                    watch(reelId)
                }
                .onFailure { e ->
                    _state.value = _state.value.copy(
                        busy = false, upload = UploadPhase.Refused, error = e.toFailure(),
                    )
                }
        }
    }

    /**
     * Retries a failed upload against the draft that already exists.
     *
     * It reuses the stored presign rather than creating a second draft. A retry
     * that re-drafted would leave the first listing row orphaned and its reel
     * waiting for bytes that never come — one abandoned listing per dropped
     * connection, which on a cell network is a lot of them. The URL is valid for
     * the upload TTL; past that the store returns 403 and this fails again with
     * something the seller can read.
     */
    fun retryUpload() {
        val reelId = _state.value.reelId ?: return
        val bytes = clip ?: return
        val url = pendingUploadUrl ?: return
        if (_state.value.busy) return

        _state.value = _state.value.copy(upload = UploadPhase.Uploading, error = null, busy = true)
        viewModelScope.launch {
            runCatching { api.uploadVideo(url, pendingUploadHeaders, bytes) }
                .onFailure { e ->
                    _state.value = _state.value.copy(
                        busy = false, upload = UploadPhase.Failed, error = e.toFailure(),
                    )
                    return@launch
                }
            _state.value = _state.value.copy(upload = UploadPhase.Processing)
            runCatching { api.publish(reelId) }
                .onSuccess {
                    _state.value = _state.value.copy(busy = false, status = it, upload = UploadPhase.Done)
                    watch(reelId)
                }
                .onFailure { e ->
                    // Metadata can only be submitted before moderation begins, so
                    // a publish that 409s here means the pipeline already started
                    // — which is a success the seller should be shown, not an
                    // error. Refreshing the status says which it was.
                    _state.value = _state.value.copy(busy = false, error = e.toFailure())
                    refreshStatus()
                }
        }
    }

    /**
     * Polls the reel while it is still moving.
     *
     * Polling rather than a socket: this screen is open for a minute at most and
     * a connection held for it costs more than five requests. It stops the moment
     * the state is one the pipeline will not leave on its own — a rejected reel
     * polled forever is a battery drain that reports nothing new.
     */
    private fun watch(reelId: String) {
        pollJob?.cancel()
        pollJob = viewModelScope.launch {
            repeat(MAX_POLLS) {
                delay(POLL_INTERVAL_MS)
                val status = runCatching { api.status(reelId) }.getOrNull() ?: return@repeat
                _state.value = _state.value.copy(status = status)
                if (status.state in SETTLED_STATES) return@launch
            }
        }
    }

    fun refreshStatus() {
        val reelId = _state.value.reelId ?: return
        viewModelScope.launch {
            runCatching { api.status(reelId) }
                .onSuccess { _state.value = _state.value.copy(status = it) }
        }
    }

    /** Starts over, releasing the clip. */
    fun reset() {
        pollJob?.cancel()
        lookupJob?.cancel()
        clip = null
        pendingUploadUrl = null
        pendingUploadHeaders = emptyMap()
        _state.value = SellUiState(verticals = _state.value.verticals)
    }

    override fun onCleared() {
        clip = null
        super.onCleared()
    }

    // ── The trust document ───────────────────────────────────────────────────

    /**
     * Uploads the mulkiya, the permit scan, whatever the vertical asked for.
     *
     * Three calls in the order the reel upload uses and for the same reasons:
     * presign, PUT, confirm. The confirm is what makes the row real, and the
     * server checks the object's size rather than believing the client —
     * because a PUT that 403'd on a signature mismatch looks like a completed
     * request from here.
     *
     * Only reachable once a draft exists. There is no subject to attach a
     * document to before that, which is why the affordance is on the Live
     * screen rather than beside the form that asks for it.
     */
    fun attachDocument(kind: String, contentType: String, bytes: ByteArray) {
        val subjectId = _state.value.subjectId ?: return
        val subjectType = _state.value.subjectType
        if (subjectType.isEmpty()) return
        if (_state.value.trust.phase == DocumentPhase.Uploading) return

        _state.value = _state.value.copy(trust = TrustState(phase = DocumentPhase.Uploading))
        viewModelScope.launch {
            runCatching {
                val up = api.requestDocumentUpload(subjectType, subjectId, kind, contentType)
                api.uploadDocument(up.uploadUrl, up.uploadHeaders, bytes)
                api.confirmDocument(up.documentId)
            }
                .onSuccess {
                    // Submitted, not Done. A person still has to look at it, and
                    // telling the seller their listing is Verified before that
                    // happens is the same mistake as calling a reel live while
                    // it is with a moderator.
                    _state.value = _state.value.copy(
                        trust = TrustState(phase = DocumentPhase.Submitted),
                    )
                }
                .onFailure { e ->
                    _state.value = _state.value.copy(
                        trust = TrustState(phase = DocumentPhase.Failed, error = e.toFailure()),
                    )
                }
        }
    }

    /**
     * The picker returned something that could not be read.
     *
     * Its own entry point rather than a generic error setter, because the cause
     * is specific and so is the remedy: a provider revoked access to the URI,
     * usually after a rotation, and picking the file again fixes it.
     */
    fun onDocumentUnreadable() {
        _state.value = _state.value.copy(
            trust = TrustState(phase = DocumentPhase.Failed, error = Failure.Unknown),
        )
    }

    /** Re-reads the document's state, so a decision shows up without a reinstall. */
    fun refreshDocuments() {
        val subjectId = _state.value.subjectId ?: return
        val subjectType = _state.value.subjectType
        if (subjectType.isEmpty()) return
        viewModelScope.launch {
            runCatching { api.documentsFor(subjectType, subjectId) }
                .onSuccess { page ->
                    // The newest that has not been superseded. The list carries
                    // every attempt, because a reviewer looking at a second
                    // document wants to know there was a first — but the seller
                    // only cares about the live one.
                    val live = page.items.firstOrNull {
                        it.reason != SUPERSEDED
                    } ?: return@onSuccess

                    _state.value = _state.value.copy(
                        trust = TrustState(
                            phase = when (live.status) {
                                "approved" -> DocumentPhase.Approved
                                "rejected" -> DocumentPhase.Rejected
                                "uploaded" -> DocumentPhase.Submitted
                                else -> DocumentPhase.Idle
                            },
                            reason = live.reason,
                        ),
                    )
                }
        }
    }

    private companion object {
        /**
         * The reason the server records when a seller re-uploads.
         *
         * Matched rather than parsed, and the string is the server's — a change
         * on that side makes a superseded document reappear as a rejection with
         * a confusing reason, which is a visible bug rather than a silent one.
         */
        const val SUPERSEDED = "replaced by a newer upload"

        const val LOOKUP_DEBOUNCE_MS = 220L
        const val POLL_INTERVAL_MS = 3_000L
        /**
         * Five minutes of polling. Past that the reel is with a human, and a
         * screen the seller left open is not the place to find out.
         */
        const val MAX_POLLS = 100

        /**
         * States the pipeline will not leave without an outside event. Published
         * is the happy one; the rest are a person's decision or a dead end, and
         * polling any of them produces the same answer forever.
         */
        val SETTLED_STATES = setOf(
            "published", "review_pending", "rejected", "removed", "failed", "expired",
        )
    }
}

/**
 * The trust document's own small state machine.
 *
 * Separate from [UploadPhase], which belongs to the video, because the two run
 * at different times and fail independently: the video is uploading while the
 * seller is still on the Live screen, and the mulkiya is picked afterwards. One
 * enum for both would mean a failed document reporting itself as a failed reel.
 */
enum class DocumentPhase {
    /** Nothing offered yet, or the seller has not picked a file. */
    Idle,
    Uploading,
    /** Bytes are in, a reviewer has it. This is the resting state, not Done. */
    Submitted,
    Approved,
    Rejected,
    Failed,
}

data class TrustState(
    val phase: DocumentPhase = DocumentPhase.Idle,
    /** The refusal, in the reviewer's words. */
    val reason: String? = null,
    val error: Failure? = null,
)
