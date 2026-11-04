package ae.marketplace.mobile.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** A company that covers this person's office lunches (GET /me/companies). */
@Serializable
data class CompanyBenefit(
    @SerialName("company_id") val companyId: String,
    val name: String,
    val role: String = "employee",
    val status: String = "pending",
)

@Serializable
data class CompanyPage(val items: List<CompanyBenefit> = emptyList())
