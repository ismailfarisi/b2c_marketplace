package ae.marketplace.mobile.model

/**
 * Areas a customer can browse kitchens for without sharing their location;
 * mirrors `areas.ts` on the web. Points are approximate district centres:
 * kitchens still filter on their own delivery areas.
 */
data class MealArea(
    val id: String,
    val en: String,
    val ar: String,
    val cityEn: String,
    val cityAr: String,
    val lat: Double,
    val lng: Double,
) {
    fun name(arabic: Boolean) = if (arabic) ar else en
    fun city(arabic: Boolean) = if (arabic) cityAr else cityEn
}

object MealAreas {
    private fun dubai(id: String, en: String, ar: String, lat: Double, lng: Double) =
        MealArea(id, en, ar, "Dubai", "دبي", lat, lng)
    private fun sharjah(id: String, en: String, ar: String, lat: Double, lng: Double) =
        MealArea(id, en, ar, "Sharjah", "الشارقة", lat, lng)
    private fun abuDhabi(id: String, en: String, ar: String, lat: Double, lng: Double) =
        MealArea(id, en, ar, "Abu Dhabi", "أبوظبي", lat, lng)

    val all: List<MealArea> = listOf(
        dubai("dubai-marina", "Dubai Marina", "دبي مارينا", 25.08, 55.14),
        dubai("jlt", "Jumeirah Lake Towers (JLT)", "أبراج بحيرات جميرا", 25.069, 55.141),
        dubai("jbr", "Jumeirah Beach Residence (JBR)", "مساكن شاطئ جميرا", 25.078, 55.133),
        dubai("al-barsha", "Al Barsha", "البرشاء", 25.112, 55.196),
        dubai("al-quoz", "Al Quoz", "القوز", 25.13, 55.23),
        dubai("business-bay", "Business Bay", "الخليج التجاري", 25.186, 55.262),
        dubai("downtown", "Downtown Dubai", "وسط مدينة دبي", 25.197, 55.274),
        dubai("difc", "DIFC", "مركز دبي المالي العالمي", 25.212, 55.281),
        dubai("jumeirah", "Jumeirah", "جميرا", 25.21, 55.25),
        dubai("satwa", "Al Satwa", "السطوة", 25.23, 55.28),
        dubai("karama", "Al Karama", "الكرامة", 25.244, 55.305),
        dubai("bur-dubai", "Bur Dubai", "بر دبي", 25.255, 55.297),
        dubai("deira", "Deira", "ديرة", 25.271, 55.309),
        dubai("al-qusais", "Al Qusais", "القصيص", 25.276, 55.38),
        dubai("al-nahda-dubai", "Al Nahda (Dubai)", "النهدة (دبي)", 25.29, 55.37),
        dubai("mirdif", "Mirdif", "مردف", 25.22, 55.42),
        dubai("silicon-oasis", "Dubai Silicon Oasis", "واحة دبي للسيليكون", 25.118, 55.383),
        dubai("international-city", "International City", "المدينة العالمية", 25.163, 55.41),
        dubai("jvc", "Jumeirah Village Circle (JVC)", "قرية جميرا الدائرية", 25.058, 55.207),
        dubai("sports-city", "Dubai Sports City", "مدينة دبي الرياضية", 25.04, 55.22),
        dubai("motor-city", "Motor City", "موتور سيتي", 25.046, 55.236),
        dubai("discovery-gardens", "Discovery Gardens", "ديسكفري جاردنز", 25.039, 55.143),
        sharjah("al-majaz", "Al Majaz", "المجاز", 25.326, 55.383),
        sharjah("al-nahda-sharjah", "Al Nahda (Sharjah)", "النهدة (الشارقة)", 25.302, 55.373),
        sharjah("al-taawun", "Al Taawun", "التعاون", 25.315, 55.37),
        abuDhabi("al-reem", "Al Reem Island", "جزيرة الريم", 24.497, 54.405),
        abuDhabi("khalidiyah", "Al Khalidiyah", "الخالدية", 24.47, 54.35),
        abuDhabi("khalifa-city", "Khalifa City", "مدينة خليفة", 24.42, 54.58),
    )

    fun search(query: String): List<MealArea> {
        val q = query.trim()
        if (q.isEmpty()) return all
        return all.filter { it.en.contains(q, ignoreCase = true) || it.ar.contains(q) }
    }
}
