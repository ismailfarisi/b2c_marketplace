package media

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePoster renders a test image to a temporary JPEG and returns its path.
//
// JPEG rather than PNG deliberately: that is what ffmpeg writes, and a lossy
// codec is exactly the thing that would break an encoder written against exact
// pixel values.
func writePoster(t *testing.T, img image.Image) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "poster.jpg")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return path
}

func solid(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

// halves paints the top half one colour and the bottom half another, which is
// the cheapest image with a real vertical gradient to encode.
func halves(w, h int, top, bottom color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		c := top
		if y >= h/2 {
			c = bottom
		}
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

const alphabet = blurAlphabet

func TestTheHashIsTheRightShape(t *testing.T) {
	// Length is derivable from the component counts: 4 + 2*x*y. A hash of any
	// other length is refused by the client decoder, so getting this wrong
	// means every placeholder silently disappears.
	path := writePoster(t, solid(120, 200, color.RGBA{R: 40, G: 90, B: 160, A: 255}))

	hash, err := EncodeBlurHash(path, PosterComponentsX, PosterComponentsY)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	want := 4 + 2*PosterComponentsX*PosterComponentsY
	if len(hash) != want {
		t.Fatalf("hash %q has length %d, want %d", hash, len(hash), want)
	}
	for i, r := range hash {
		if !strings.ContainsRune(alphabet, r) {
			t.Fatalf("character %d of %q is outside base 83", i, hash)
		}
	}
}

func TestTheFirstCharacterEncodesTheComponentCount(t *testing.T) {
	// The decoder reads the grid size from here and rejects the hash if the
	// length disagrees. This is the one byte that makes the rest parseable.
	path := writePoster(t, solid(64, 64, color.RGBA{R: 200, G: 30, B: 30, A: 255}))

	for _, tc := range []struct{ x, y int }{{4, 3}, {3, 3}, {1, 1}, {9, 9}} {
		hash, err := EncodeBlurHash(path, tc.x, tc.y)
		if err != nil {
			t.Fatalf("%dx%d: %v", tc.x, tc.y, err)
		}
		flag := strings.IndexRune(alphabet, rune(hash[0]))
		if got, want := flag%9+1, tc.x; got != want {
			t.Errorf("%dx%d: encoded x=%d", tc.x, tc.y, got)
		}
		if got, want := flag/9+1, tc.y; got != want {
			t.Errorf("%dx%d: encoded y=%d", tc.x, tc.y, got)
		}
		if len(hash) != 4+2*tc.x*tc.y {
			t.Errorf("%dx%d: length %d", tc.x, tc.y, len(hash))
		}
	}
}

func TestTheAverageColourSurvivesTheRoundTrip(t *testing.T) {
	// The DC term is the whole placeholder for a flat image, and it is also
	// what the client's averageColour() reads on its own. If sRGB conversion is
	// wrong in either direction this is where it shows: a mid blue comes back
	// as a washed-out or a muddy one, and the error is invisible unless
	// measured.
	want := color.RGBA{R: 40, G: 90, B: 160, A: 255}
	path := writePoster(t, solid(120, 200, want))

	hash, err := EncodeBlurHash(path, PosterComponentsX, PosterComponentsY)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	// Decoding the DC term, the same way the Kotlin client does.
	dc := decode83(t, hash[2:6])
	got := color.RGBA{
		R: uint8((dc >> 16) & 0xFF),
		G: uint8((dc >> 8) & 0xFF),
		B: uint8(dc & 0xFF),
	}

	// Tolerance covers JPEG at quality 90 plus two sRGB conversions. Ten is
	// tight enough to catch a missing linearisation, which shifts a mid tone by
	// forty or more.
	for _, c := range []struct {
		name      string
		got, want uint8
	}{{"r", got.R, want.R}, {"g", got.G, want.G}, {"b", got.B, want.B}} {
		if diff := int(c.got) - int(c.want); diff > 10 || diff < -10 {
			t.Errorf("%s: got %d, want %d", c.name, c.got, c.want)
		}
	}
}

// The second character of a hash is the quantised maximum AC amplitude, and it
// is the honest measure of "how much detail is in here".
//
// The first version of these two tests compared the encoded AC *digits*
// instead, expecting a flat image to produce identical ones. That was wrong,
// and the way it is wrong is worth recording because it looks like a bug in the
// encoder and is not.
//
// BlurHash quantises every AC term as a fraction of the largest AC term, not on
// an absolute scale. A perfectly flat image still has a tiny numerical residue
// — the odd harmonics of a constant sum to exactly 1 over the sampling grid
// rather than to 0 — and dividing that residue by a maximum which is itself
// that residue produces digits at the extremes of the range. The *decoded*
// amplitude is a thousandth of a shade, so the reconstruction is flat as
// intended; only the digits look dramatic.
//
// So the property to assert is the amplitude, which is exactly what this byte
// carries.
func detailLevel(t *testing.T, hash string) int {
	t.Helper()
	return decode83(t, hash[1:2])
}

func TestAFlatImageEncodesAlmostNoDetail(t *testing.T) {
	path := writePoster(t, solid(100, 100, color.RGBA{R: 128, G: 128, B: 128, A: 255}))

	hash, err := EncodeBlurHash(path, 3, 3)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	// Out of a possible 82. Anything above single digits here would mean real
	// contrast is being reconstructed from an image that has none.
	if got := detailLevel(t, hash); got > 3 {
		t.Fatalf("a solid colour encoded detail level %d: %q", got, hash)
	}
}

func TestAnImageWithStructureEncodesRealDetail(t *testing.T) {
	// The converse, so the test above cannot be satisfied by an encoder that
	// emits nothing — which is precisely what the empty-string behaviour this
	// replaced amounted to.
	path := writePoster(t, halves(100, 100,
		color.RGBA{R: 250, G: 250, B: 250, A: 255},
		color.RGBA{R: 5, G: 5, B: 5, A: 255},
	))

	hash, err := EncodeBlurHash(path, 3, 3)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	if got := detailLevel(t, hash); got < 20 {
		t.Fatalf("a black-and-white split encoded detail level %d: %q", got, hash)
	}

	// And the detail must be on the axis it actually varies along. A horizontal
	// term dominating a vertically-split image means the basis functions have
	// their axes swapped — which produces plausible-looking blurs that are
	// rotated ninety degrees from the photograph, and is invisible in any test
	// that only measures magnitude.
	//
	// Components run i (x) fastest: with x=3, ac[0] is (i=1,j=0) — purely
	// horizontal — and ac[2] is (i=0,j=1) — purely vertical.
	horizontal := decode83(t, hash[6:8])
	vertical := decode83(t, hash[10:12])
	if distanceFromFlat(vertical) <= distanceFromFlat(horizontal) {
		t.Fatalf("a top/bottom split encoded more horizontal than vertical detail: %q", hash)
	}
}

// distanceFromFlat measures how far an encoded AC triple sits from the
// zero-midpoint, which each channel encodes as 9 out of 0..18.
func distanceFromFlat(encoded int) int {
	r := encoded / (19 * 19)
	g := (encoded / 19) % 19
	b := encoded % 19
	abs := func(v int) int {
		if v < 0 {
			return -v
		}
		return v
	}
	return abs(r-9) + abs(g-9) + abs(b-9)
}

func TestComponentsOutsideTheRangeAreRefused(t *testing.T) {
	// Base-83 encodes the grid in one character, so anything above 9 is not
	// representable. Refusing beats emitting a hash no decoder will accept.
	path := writePoster(t, solid(32, 32, color.RGBA{A: 255}))

	for _, tc := range []struct{ x, y int }{{0, 3}, {4, 0}, {10, 3}, {4, 10}, {-1, 3}} {
		if _, err := EncodeBlurHash(path, tc.x, tc.y); err == nil {
			t.Errorf("%dx%d was accepted", tc.x, tc.y)
		}
	}
}

func TestAMissingOrUndecodablePosterIsAnErrorNotAPanic(t *testing.T) {
	// This runs on the transcode path against a file ffmpeg was supposed to
	// have written. When it did not, the caller logs and carries on — but only
	// if this returns rather than panicking.
	if _, err := EncodeBlurHash(filepath.Join(t.TempDir(), "absent.jpg"), 4, 3); err == nil {
		t.Error("a missing poster was accepted")
	}

	junk := filepath.Join(t.TempDir(), "poster.jpg")
	if err := os.WriteFile(junk, []byte("not a jpeg"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeBlurHash(junk, 4, 3); err == nil {
		t.Error("a corrupt poster was accepted")
	}
}

func TestPortraitPostersGetMoreHorizontalDetail(t *testing.T) {
	// Reels are 9:16. Documenting the choice as a test so that "make it 4x4
	// like the examples" is a decision someone has to argue with rather than
	// make by accident.
	if PosterComponentsX <= PosterComponentsY {
		t.Fatalf("poster basis is %dx%d; portrait stills want more x than y",
			PosterComponentsX, PosterComponentsY)
	}
}

func decode83(t *testing.T, s string) int {
	t.Helper()
	v := 0
	for _, r := range s {
		i := strings.IndexRune(alphabet, r)
		if i < 0 {
			t.Fatalf("character %q outside base 83", r)
		}
		v = v*83 + i
	}
	return v
}
