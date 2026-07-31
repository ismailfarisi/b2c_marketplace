package ae.marketplace.mobile.me

/**
 * What a buyer can narrow the classifieds index by.
 *
 * # Why this exists as a type
 *
 * The API has accepted price bounds, a year range, a mileage ceiling, regional
 * spec, body type, transmission, fuel type, seller kind and four sort orders
 * since Phase 6. The app sent a text string and a location. Everything else was
 * reachable only from the web listing pages — which is to say, reachable to
 * nobody using the product as it is meant to be used.
 *
 * That gap is not cosmetic on this particular screen. Search is the *intent*
 * surface: the feed is where you discover a car, and this is where you already
 * know you want a GCC-spec Patrol under 200,000. Without filters the answer to
 * that question is a list you scroll, which is the experience of every
 * classifieds site this product is trying to beat.
 *
 * # Why every field is nullable rather than zero-defaulted
 *
 * Because zero is a real number in most of them. `priceMinMinor = 0` and "no
 * minimum price" are the same value and different questions, and a filter that
 * cannot tell them apart sends `price_min_minor=0` on every request — which is
 * harmless here and would not be for `yearMax`.
 */
data class SearchFilters(
    /** Minor units, matching the wire and `internal/money`. */
    val priceMinMinor: Long? = null,
    val priceMaxMinor: Long? = null,
    val yearMin: Int? = null,
    val yearMax: Int? = null,
    /** A ceiling only. Nobody has ever searched for a car with *at least* 80,000 km. */
    val mileageMax: Int? = null,
    val regionalSpecs: Set<String> = emptySet(),
    val bodyTypes: Set<String> = emptySet(),
    val transmissions: Set<String> = emptySet(),
    /** private | dealer. Null means both. */
    val sellerKind: String? = null,
    /** Null means the server's default, which is newest first. */
    val sort: String? = null,
) {
    /**
     * How many facets are narrowing the result set.
     *
     * Shown on the filter button, because a filter that is silently still
     * applied is the single most common way a search screen lies to someone:
     * they set a price cap, search something else, and conclude the new thing
     * does not exist.
     */
    val activeCount: Int
        get() = listOf(
            priceMinMinor != null || priceMaxMinor != null,
            yearMin != null || yearMax != null,
            mileageMax != null,
            regionalSpecs.isNotEmpty(),
            bodyTypes.isNotEmpty(),
            transmissions.isNotEmpty(),
            sellerKind != null,
        ).count { it }

    /** Sort is not a filter — it reorders, it never hides — so it is excluded above. */
    val isEmpty: Boolean get() = activeCount == 0

    fun toggle(set: Set<String>, value: String): Set<String> =
        if (value in set) set - value else set + value

    // ── Named mutators ───────────────────────────────────────────────────────
    //
    // Kotlin has `copy`, so on the Compose side these save little. They exist
    // for Swift, where `copy` on a data class exports as a `doCopy` taking
    // *every* property — so changing one facet from SwiftUI means restating the
    // other nine, and the compiler cannot tell you when you restate one wrongly.
    // The first version of the iOS filter sheet did exactly that and it was
    // forty lines of transcription with a bug in it.
    //
    // Named methods are also just better at the call site on both platforms:
    // `filters.withPriceMax(200_000_00)` says what is happening and
    // `filters.copy(priceMaxMinor = 200_000_00)` says how.

    fun withPriceMin(minor: Long?) = copy(priceMinMinor = minor)
    fun withPriceMax(minor: Long?) = copy(priceMaxMinor = minor)
    fun withYearMin(year: Int?) = copy(yearMin = year)
    fun withYearMax(year: Int?) = copy(yearMax = year)
    fun withMileageMax(km: Int?) = copy(mileageMax = km)

    fun toggleSpec(value: String) = copy(regionalSpecs = toggle(regionalSpecs, value))
    fun toggleBodyType(value: String) = copy(bodyTypes = toggle(bodyTypes, value))
    fun toggleTransmission(value: String) = copy(transmissions = toggle(transmissions, value))

    /** Single-select: choosing the active one clears it back to "both". */
    fun toggleSellerKind(value: String) =
        copy(sellerKind = if (sellerKind == value) null else value)

    fun withSort(value: String?) = copy(sort = value)

    /**
     * Everything off, except the sort.
     *
     * Sort survives a Clear because it is not a filter — it reorders, it never
     * hides — and resetting it would silently undo a choice the viewer did not
     * ask to undo.
     */
    fun cleared() = SearchFilters(sort = sort)
}

/**
 * The sort orders the server implements.
 *
 * Named here rather than fetched, unlike the feed's verticals, and the
 * difference is deliberate. A vertical is data — a new one appears when a
 * provider registers, and hard-coding the list would mean an app release to
 * launch property. A sort order is behaviour: each one needs an index, and a
 * server that grew a fifth would need this build to know what to call it
 * anyway.
 *
 * An unrecognised value from a future server is simply not offered, and the
 * server defaults it — which is the same fallback the feed surfaces use.
 */
enum class SearchSort(val wire: String) {
    /** The server's default when none is sent, and when one it does not know is. */
    Newest("newest"),
    PriceAsc("price_asc"),
    PriceDesc("price_desc"),
    MileageAsc("mileage_asc"),
    YearDesc("year_desc"),

    /**
     * Nearest first. Only offered when the client has a location — the server
     * silently falls back to Newest without one, and a sort that appears
     * selected while producing a different order is worse than one that is not
     * offered.
     */
    Distance("distance"),
}
