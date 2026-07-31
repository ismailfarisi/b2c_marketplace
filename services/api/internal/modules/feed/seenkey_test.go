package feed

import "testing"

// The seen-set exists to stop one feed repeating as it pages. Every test here
// is a case where it was instead acting as a memory that followed the viewer
// across a change of intent — and each one presents as an empty screen on a
// surface the viewer deliberately narrowed to.

func TestNarrowingToAVerticalIsANewView(t *testing.T) {
	all := Query{ViewerKey: "d:abc", Surface: SurfaceForYou, Lat: 25.08, Lng: 55.14}
	cars := all
	cars.SubjectTypes = []string{"vehicle_listing"}

	if all.seenKey() == cars.seenKey() {
		t.Fatal("filtering reused the unfiltered feed's memory; the car just scrolled past would be excluded")
	}
}

func TestMovingTheFeedIsANewView(t *testing.T) {
	// Found by a meal plan vanishing: it was shown at Dubai Marina, the viewer
	// moved the feed to Al Barsha to find kitchens there, and the one kitchen
	// that serves both was excluded as already seen.
	marina := Query{ViewerKey: "d:abc", Surface: SurfaceForYou, Lat: 25.0805, Lng: 55.1403}
	barsha := Query{ViewerKey: "d:abc", Surface: SurfaceForYou, Lat: 25.1120, Lng: 55.1960}

	if marina.seenKey() == barsha.seenKey() {
		t.Fatal("moving the feed reused the previous location's memory")
	}
}

func TestGPSJitterDoesNotForgetThePage(t *testing.T) {
	// The opposite failure. A stationary phone's fix wanders by tens of metres;
	// if that minted a new key the feed would forget what it had shown and
	// repeat itself every few pages, which is the bug the seen-set exists for.
	still := Query{ViewerKey: "d:abc", Surface: SurfaceForYou, Lat: 25.0805, Lng: 55.1403}
	wobbled := still
	wobbled.Lat, wobbled.Lng = 25.0807, 55.1401

	if still.seenKey() != wobbled.seenKey() {
		t.Fatalf("a 20m drift changed the key: %q vs %q", still.seenKey(), wobbled.seenKey())
	}
}

func TestSurfacesDoNotShareMemory(t *testing.T) {
	forYou := Query{ViewerKey: "d:abc", Surface: SurfaceForYou, Lat: 25.08, Lng: 55.14}
	nearby := forYou
	nearby.Surface = SurfaceNearby

	if forYou.seenKey() == nearby.seenKey() {
		t.Fatal("two tabs shared one seen-set")
	}
}

func TestVerticalOrderDoesNotSplitTheView(t *testing.T) {
	// ?subject_type=a&subject_type=b and the reverse are one question. Two keys
	// would give the same view two half-remembered histories.
	ab := Query{ViewerKey: "d:abc", Surface: SurfaceForYou, Lat: 25.08, Lng: 55.14,
		SubjectTypes: []string{"meal_plan", "vehicle_listing"}}
	ba := ab
	ba.SubjectTypes = []string{"vehicle_listing", "meal_plan"}

	if ab.seenKey() != ba.seenKey() {
		t.Fatal("the same filter in two orders produced two views")
	}
}

func TestAnAnonymousViewerWithNoKeyIsNotTracked(t *testing.T) {
	// No viewer key means no seen-set at all, rather than everyone sharing one.
	q := Query{Surface: SurfaceForYou, Lat: 25.08, Lng: 55.14}
	if q.seenKey() != "" {
		t.Fatalf("expected no key, got %q", q.seenKey())
	}
}
