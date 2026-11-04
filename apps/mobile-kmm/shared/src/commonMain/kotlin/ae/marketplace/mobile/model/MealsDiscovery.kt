package ae.marketplace.mobile.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** One tile of a cuisine_strip section. */
@Serializable
data class CuisineTile(
    val cuisine: String,
    val kitchens: Int = 0,
    @SerialName("image_url") val imageUrl: String? = null,
)

/** GET /meals/collections/{slug}: the collection and the kitchens in it that deliver here. */
@Serializable
data class CollectionKitchens(
    val slug: String,
    val title: String,
    @SerialName("title_ar") val titleAr: String? = null,
    val subtitle: String? = null,
    @SerialName("subtitle_ar") val subtitleAr: String? = null,
    val kitchens: List<Kitchen> = emptyList(),
)
