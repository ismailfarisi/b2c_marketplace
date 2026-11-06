package ae.marketplace.mobile.meals

enum class MealsLinkPage {
    HOME, KITCHEN, MINE, SUBSCRIPTION, ROTATION,
    /** One rotation subscription (`/rotations/{id}`). */
    ROTATION_DETAIL,
    /** /rotations/{id}/renew: the renewal screen with the proposed kitchens. */
    ROTATION_RENEW,
    /** Help: the ticket list, or one ticket when [MealsLink.id] is set. Web-only today. */
    SUPPORT,
    /** A merchandised collection (`/collections/{slug}`). */
    COLLECTION,
}

/** A screen in the meals tab that a web URL or push `link` points at. */
data class MealsLink(val page: MealsLinkPage, val id: String? = null)

/**
 * Maps meals web URLs (App Links, Universal Links, push `link` values) to the
 * app screen showing the same thing. Accepts full URLs or bare paths, with or
 * without the /en or /ar prefix. Anything else returns null and is ignored.
 */
object MealsLinks {
    fun parse(url: String): MealsLink? {
        var path = url.trim()
        val scheme = path.indexOf("://")
        if (scheme >= 0) {
            val slash = path.indexOf('/', scheme + 3)
            path = if (slash < 0) "/" else path.substring(slash)
        }
        path = path.substringBefore('#').substringBefore('?')
        var parts = path.split('/').filter { it.isNotEmpty() }
        if (parts.firstOrNull() == "en" || parts.firstOrNull() == "ar") parts = parts.drop(1)
        val id = parts.getOrNull(1)?.takeIf { it.isNotBlank() }
        return when (parts.firstOrNull()) {
            null -> MealsLink(MealsLinkPage.HOME)
            "kitchens" -> id?.let { MealsLink(MealsLinkPage.KITCHEN, it) } ?: MealsLink(MealsLinkPage.HOME)
            "subscriptions" -> if (id == null) MealsLink(MealsLinkPage.MINE) else MealsLink(MealsLinkPage.SUBSCRIPTION, id)
            "rotations" -> when {
                id == null -> MealsLink(MealsLinkPage.MINE)
                parts.getOrNull(2) == "renew" -> MealsLink(MealsLinkPage.ROTATION_RENEW, id)
                else -> MealsLink(MealsLinkPage.ROTATION_DETAIL, id)
            }
            "support" -> MealsLink(MealsLinkPage.SUPPORT, id)
            "collections" -> id?.let { MealsLink(MealsLinkPage.COLLECTION, it) } ?: MealsLink(MealsLinkPage.HOME)
            "rotation" -> MealsLink(MealsLinkPage.ROTATION)
            else -> null
        }
    }
}
