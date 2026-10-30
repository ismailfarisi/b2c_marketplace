package ae.marketplace.mobile.model

/** Display names for cuisine and dietary slugs; mirrors `tagLabel` on the web. */
object MealTags {
    private val names: Map<String, Pair<String, String>> = mapOf(
        "indian" to ("Indian" to "هندي"),
        "kerala" to ("Kerala" to "كيرلا"),
        "south_indian" to ("South Indian" to "جنوب هندي"),
        "pakistani" to ("Pakistani" to "باكستاني"),
        "arabic" to ("Arabic" to "عربي"),
        "emirati" to ("Emirati" to "إماراتي"),
        "lebanese" to ("Lebanese" to "لبناني"),
        "egyptian" to ("Egyptian" to "مصري"),
        "filipino" to ("Filipino" to "فلبيني"),
        "mediterranean" to ("Mediterranean" to "متوسطي"),
        "asian" to ("Asian" to "آسيوي"),
        "chinese" to ("Chinese" to "صيني"),
        "continental" to ("Continental" to "أوروبي"),
        "healthy" to ("Healthy" to "صحي"),
        "halal" to ("Halal" to "حلال"),
        "vegetarian" to ("Vegetarian" to "نباتي"),
        "vegan" to ("Vegan" to "نباتي صرف"),
        "gluten_free" to ("Gluten-free" to "خالٍ من الغلوتين"),
        "dairy_free" to ("Dairy-free" to "خالٍ من الألبان"),
    )

    fun label(tag: String, arabic: Boolean): String {
        val key = tag.trim().lowercase().replace(Regex("[\\s-]+"), "_")
        names[key]?.let { return if (arabic) it.second else it.first }
        return tag.replace(Regex("[_-]+"), " ").split(" ")
            .joinToString(" ") { w -> w.replaceFirstChar { it.uppercaseChar() } }
    }

    /** Cuisines then dietary tags, as one " · " line. */
    fun line(tags: List<String>, arabic: Boolean): String =
        tags.joinToString(" · ") { label(it, arabic) }
}
