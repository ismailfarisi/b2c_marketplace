package ae.marketplace.mobile.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/*
 * The sell flow's wire format.
 *
 * Hand-written from api/openapi.yaml, same as Feed.kt and with the same caveat:
 * this is a copy of the spec's schemas and it can drift. It is one file and
 * short, and `make gen-kotlin` deletes it.
 *
 * What is worth reading closely is how little of it is about cars. There is no
 * mileage field, no price field and no make picker anywhere in this file or in
 * the screens that render it — the whole form arrives as [SellField] values the
 * client draws without knowing what any of them mean. That is the difference
 * between shipping property in a Go file and shipping it in an app release.
 */

/** One tile in the "What are you listing?" grid. */
@Serializable
data class SellVertical(
    /** Sent back as `subject_type`. Opaque; never rendered. */
    val key: String,
    /** The vertical's own word for itself — "Cars", not "vehicle_listing". */
    val label: String,
    @SerialName("label_ar") val labelAr: String = "",
    /**
     * A glyph name from a closed set the client ships: car, home, meal, moon,
     * tag. The one place a client legitimately maps a server value onto
     * something, and it is safe because the mapping is on the *glyph* — a new
     * vertical picks an existing name and needs no release. Unrecognised names
     * fall back rather than drawing nothing.
     */
    val icon: String = "",
)

@Serializable
data class SellVerticals(val verticals: List<SellVertical> = emptyList())

/**
 * Everything a vertical needs to know before it can be listed.
 *
 * The client renders this and posts back a map. It does not validate against a
 * schema it compiled in, it does not reorder the fields, and it does not decide
 * which of them are relevant — all three would be it having an opinion about a
 * vertical, which is the opinion this seam exists to keep on the server.
 */
@Serializable
data class SellForm(
    val fields: List<SellField> = emptyList(),
    val clocks: List<ClockOption> = emptyList(),
    val trust: TrustUpload? = null,
    /** The line over the viewfinder. The vertical's own advice. */
    val tip: String = "",
    @SerialName("tip_ar") val tipAr: String = "",
)

@Serializable
data class SellField(
    val key: String,
    val label: String,
    @SerialName("label_ar") val labelAr: String = "",
    /** text | integer | money | select | lookup. Unknown kinds render as text. */
    val kind: String,
    val required: Boolean = false,
    /** Sits beside the input — "km", "AED". */
    val unit: String = "",
    @SerialName("unit_ar") val unitAr: String = "",
    /** The sub-line, where a vertical explains a term the seller may not know. */
    val hint: String = "",
    @SerialName("hint_ar") val hintAr: String = "",
    val placeholder: String = "",
    val options: List<SellOption> = emptyList(),
    /** For `lookup`: the path to query with ?q=. Served by the owning vertical. */
    val source: String = "",
    /**
     * Bounds, enforced locally so a bad value is caught before the round trip.
     * The server re-checks: a client-side bound is a courtesy, never a control.
     */
    val min: Long? = null,
    val max: Long? = null,
)

@Serializable
data class SellOption(
    val value: String,
    val label: String,
    @SerialName("label_ar") val labelAr: String = "",
)

/**
 * One answer to "how long should it run".
 *
 * The listing type, not a duration picker. An open listing, an auction and a
 * flash window are different products with different overlays, which is why
 * [mode] is here and why the client renders [detail] — a seller choosing between
 * listing types is choosing what their buyers will see, and this is the only
 * place they find that out before publishing.
 */
@Serializable
data class ClockOption(
    val key: String,
    /** countdown | window | freshness | cutoff. Matches TimeSignal.mode. */
    val mode: String,
    val label: String,
    @SerialName("label_ar") val labelAr: String = "",
    val detail: String = "",
    @SerialName("detail_ar") val detailAr: String = "",
    /** This term runs between two instants the seller picks, not for a duration. */
    @SerialName("needs_window") val needsWindow: Boolean = false,
    val default: Boolean = false,
)

/**
 * The document that earns a badge, and the identifier that travels with it.
 *
 * Optional for a car: skipping the mulkiya publishes normally, the listing is
 * worth less, and that is the whole incentive.
 *
 * Not optional for a property advert, where the permit is a legal precondition
 * — but the *client* is told that by [reference] being required rather than by
 * knowing what a permit is. That distinction is the seam: this screen renders a
 * document row and possibly a text box, and has no idea one vertical's row is a
 * nice-to-have and another's is a regulator.
 */
@Serializable
data class TrustUpload(
    val key: String,
    val label: String,
    @SerialName("label_ar") val labelAr: String = "",
    /** The badge it buys. Rendered as the reward. */
    val earns: String = "",
    @SerialName("earns_ar") val earnsAr: String = "",
    val accept: List<String> = emptyList(),
    /**
     * An identifier that belongs to the document, when it has one.
     *
     * Drawn with the same code as the four fields above, because it is one —
     * it posts back through the same values map under its own key. Null for a
     * vertical whose trust artefact is just a photograph.
     */
    val reference: SellField? = null,
)

/** What the server posts back when the subject and the reel draft exist. */
@Serializable
data class SellDraft(
    @SerialName("reel_id") val reelId: String,
    @SerialName("subject_id") val subjectId: String,
    @SerialName("subject_type") val subjectType: String = "",
    @SerialName("upload_url") val uploadUrl: String,
    @SerialName("upload_headers") val uploadHeaders: Map<String, String> = emptyMap(),
    @SerialName("expires_at") val expiresAt: String = "",
    /**
     * Rendered on the live screen, verbatim.
     *
     * Every author on this path is new, so all of it goes to a human before it
     * appears in the feed. A seller who expects their video live in ninety
     * seconds and gets it in two hours writes a one-star review about a working
     * system, and this sentence is what prevents that.
     */
    @SerialName("review_note") val reviewNote: String = "",
)

/**
 * The live screen.
 *
 * [state] is the reel's pipeline state, reported raw. The client turns it into a
 * sentence; it must not turn `review_pending` into "live", because the seller
 * would share the link, their friend would see nothing, and they would conclude
 * the platform is broken.
 */
@Serializable
data class SellStatus(
    val id: String,
    val state: String,
    val counters: Counters = Counters(),
    @SerialName("published_at") val publishedAt: String? = null,
    @SerialName("subject_state") val subjectState: String = "",
    val failure: SellFailure? = null,
)

@Serializable
data class SellFailure(val code: String = "", val detail: String = "")

@Serializable
data class LookupOptions(val options: List<SellOption> = emptyList())
