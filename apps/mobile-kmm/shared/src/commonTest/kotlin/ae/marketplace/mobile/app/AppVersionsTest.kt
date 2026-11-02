package ae.marketplace.mobile.app

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class AppVersionsTest {
    @Test fun numericCompare() {
        assertTrue(AppVersions.compare("1.10.0", "1.9.3") > 0)
        assertEquals(0, AppVersions.compare("1.2", "1.2.0"))
        assertTrue(AppVersions.compare("0.1.0-debug", "0.2") < 0)
    }

    @Test fun belowMinimumIsRequired() =
        assertEquals(UpdateNeed.REQUIRED, AppVersions.need("1.0.0", "1.1.0", "1.2.0"))

    @Test fun belowLatestIsRecommended() =
        assertEquals(UpdateNeed.RECOMMENDED, AppVersions.need("1.1.0", "1.1.0", "1.2.0"))

    @Test fun unconfiguredIsUpToDate() =
        assertEquals(UpdateNeed.UP_TO_DATE, AppVersions.need("0.1.0", "", ""))
}
