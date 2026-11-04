package ae.marketplace.mobile.meals

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull
import kotlin.test.assertNull

class MealsLinksTest {
    @Test fun kitchenFromFullUrl() = assertEquals(
        MealsLink(MealsLinkPage.KITCHEN, "b1"),
        MealsLinks.parse("https://meals.example/ar/kitchens/b1/subscribe/p2?ref=x"),
    )

    @Test fun subscriptionFromPushPath() = assertEquals(
        MealsLink(MealsLinkPage.SUBSCRIPTION, "s9"),
        MealsLinks.parse("/en/subscriptions/s9"),
    )

    @Test fun mySubscriptions() = assertEquals(MealsLink(MealsLinkPage.MINE), MealsLinks.parse("/subscriptions"))

    @Test fun rootIsHome() = assertEquals(MealsLink(MealsLinkPage.HOME), MealsLinks.parse("https://meals.example"))

    @Test fun rotation() = assertEquals(MealsLink(MealsLinkPage.ROTATION), MealsLinks.parse("/ar/rotation#top"))

    @Test fun otherPagesIgnored() {
        assertNull(MealsLinks.parse("https://meals.example/en/admin/users"))
        assertNull(MealsLinks.parse("/en/console"))
    }

    @Test fun rotationList() = assertEquals(MealsLink(MealsLinkPage.MINE), MealsLinks.parse("/en/rotations"))

    @Test fun rotationDetail() = assertEquals(
        MealsLink(MealsLinkPage.ROTATION_DETAIL, "r7"),
        MealsLinks.parse("https://meals.example/ar/rotations/r7?x=1"),
    )

    @Test fun supportTicket() = assertEquals(MealsLink(MealsLinkPage.SUPPORT, "t3"), MealsLinks.parse("/en/support/t3"))

    @Test fun supportList() = assertEquals(MealsLink(MealsLinkPage.SUPPORT), MealsLinks.parse("/support"))

    @Test fun collection() = assertEquals(
        MealsLink(MealsLinkPage.COLLECTION, "office-lunch"),
        MealsLinks.parse("https://meals.example/ar/collections/office-lunch?utm=x"),
    )

    @Test fun collectionWithoutSlugIsHome() = assertEquals(MealsLink(MealsLinkPage.HOME), MealsLinks.parse("/en/collections"))

    /**
     * One sample per App Link / Universal Link path (AndroidManifest.xml
     * prefixes and the AASA route's PATHS). Each must open a screen, or the
     * OS hands the app a link it then drops.
     */
    @Test fun appLinkPathsOpenAScreen() {
        listOf(
            "/en/kitchens/b1", "/ar/kitchens/b1",
            "/en/subscriptions", "/ar/subscriptions/s1",
            "/en/rotation", "/ar/rotations", "/en/rotations/r1",
        ).forEach { assertNotNull(MealsLinks.parse("https://meals.example$it"), it) }
    }
}
