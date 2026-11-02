package ae.marketplace.mobile.meals

import kotlin.test.Test
import kotlin.test.assertEquals
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
}
