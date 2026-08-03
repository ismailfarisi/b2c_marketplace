package media

import (
	"fmt"
	"image"
	"math"
	"os"

	// Registered for their side effect: the poster is a JPEG today, but this
	// function takes whatever ffmpeg was told to write, and a PNG or a WebP
	// arriving here should decode rather than fail.
	_ "image/jpeg"
	_ "image/png"
)

// EncodeBlurHash turns a poster into the ~30-character placeholder the clients
// draw while the real image loads.
//
// # Why this is worth having
//
// The `lqip` column has existed since Phase 2 and every real transcode wrote an
// empty string into it, with a comment saying a BlurHash encoder "does not
// belong in a dev transcoder". That was true of the *stub*, which invents a
// fixed hash. It was not true of the real one, and the consequence was that the
// column only ever held a value for seeded rows — so the client-side decoder,
// once written, would have had nothing to decode outside the demo data.
//
// The alternative to a placeholder is a grey rectangle for as long as the
// poster takes to arrive, on a screen whose entire job is choosing between
// things by looking at them.
//
// # Why not a library
//
// It is sixty lines of DCT against a 4x3 basis and there is no ambiguity in the
// spec. A dependency here would be a supply-chain surface on the image path for
// code that fits on a screen — and the decoder on the Kotlin side is
// hand-written for the same reason, which means the two can be tested against
// each other.
func EncodeBlurHash(path string, componentsX, componentsY int) (string, error) {
	if componentsX < 1 || componentsX > 9 || componentsY < 1 || componentsY > 9 {
		return "", fmt.Errorf("media: blurhash components out of range: %dx%d", componentsX, componentsY)
	}

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("media: open poster: %w", err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return "", fmt.Errorf("media: decode poster: %w", err)
	}

	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width == 0 || height == 0 {
		return "", fmt.Errorf("media: empty poster")
	}

	// Sampled rather than read whole. A 1080x1920 poster is two million pixels
	// and the output is thirty characters; walking all of them costs about a
	// hundred milliseconds per reel for a result that is bit-identical to
	// within rounding. The step is chosen so roughly 128 samples span each axis,
	// which is far above the Nyquist limit for a 4-component basis.
	stepX := max(1, width/blurSampleTarget)
	stepY := max(1, height/blurSampleTarget)

	// Pre-linearised samples, so the sRGB conversion runs once per sample
	// rather than once per (sample, component) pair — which for a 4x3 basis is
	// twelve times.
	type sample struct{ r, g, b float64 }
	var samples []sample
	var xs, ys []int
	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		ys = append(ys, y)
	}
	for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
		xs = append(xs, x)
	}
	samples = make([]sample, 0, len(xs)*len(ys))
	for _, y := range ys {
		for _, x := range xs {
			r, g, b, _ := img.At(x, y).RGBA()
			samples = append(samples, sample{
				// RGBA() is 16-bit premultiplied; >>8 gives the 8-bit value.
				r: srgbToLinear(int(r >> 8)),
				g: srgbToLinear(int(g >> 8)),
				b: srgbToLinear(int(b >> 8)),
			})
		}
	}

	factors := make([][3]float64, 0, componentsX*componentsY)
	for j := 0; j < componentsY; j++ {
		for i := 0; i < componentsX; i++ {
			// The DC term (i=0,j=0) is a plain average; every AC term is
			// doubled. That asymmetry is in the reference implementation and
			// getting it wrong produces an image that is correct in hue and
			// half as contrasty as it should be — which looks fine until you
			// compare it with the poster.
			scale := 2.0
			if i == 0 && j == 0 {
				scale = 1.0
			}
			var fr, fg, fb float64
			for yi, y := range ys {
				_ = y
				for xi := range xs {
					basis := math.Cos(math.Pi*float64(i)*float64(xi)/float64(len(xs))) *
						math.Cos(math.Pi*float64(j)*float64(yi)/float64(len(ys)))
					s := samples[yi*len(xs)+xi]
					fr += basis * s.r
					fg += basis * s.g
					fb += basis * s.b
				}
			}
			n := float64(len(samples))
			factors = append(factors, [3]float64{
				scale * fr / n, scale * fg / n, scale * fb / n,
			})
		}
	}

	dc := factors[0]
	ac := factors[1:]

	var hash []byte
	hash = encode83(hash, (componentsX-1)+(componentsY-1)*9, 1)

	maximumValue := 1.0
	quantisedMax := 0
	if len(ac) > 0 {
		actualMax := 0.0
		for _, f := range ac {
			for _, v := range f {
				actualMax = math.Max(actualMax, math.Abs(v))
			}
		}
		quantisedMax = int(math.Max(0, math.Min(82, math.Floor(actualMax*166-0.5))))
		maximumValue = (float64(quantisedMax) + 1) / 166
	}
	hash = encode83(hash, quantisedMax, 1)
	hash = encode83(hash, encodeDC(dc), 4)
	for _, f := range ac {
		hash = encode83(hash, encodeAC(f, maximumValue), 2)
	}

	return string(hash), nil
}

// blurSampleTarget is roughly how many samples span each axis. Well above what
// a 9-component basis can resolve, and small enough that the encode is
// microseconds rather than milliseconds.
const blurSampleTarget = 128

// PosterComponentsX and PosterComponentsY shape the hash for a portrait reel
// still.
//
// 4x3 rather than the usual 4x4: reels are 9:16, and spending an equal number
// of components on both axes gives a placeholder that is over-detailed
// horizontally and mushy where the subject actually is.
const (
	PosterComponentsX = 4
	PosterComponentsY = 3
)

func encodeDC(v [3]float64) int {
	return linearToSrgb(v[0])<<16 + linearToSrgb(v[1])<<8 + linearToSrgb(v[2])
}

func encodeAC(v [3]float64, maximumValue float64) int {
	quant := func(f float64) int {
		q := math.Floor(signPow(f/maximumValue, 0.5)*9 + 9.5)
		return int(math.Max(0, math.Min(18, q)))
	}
	return quant(v[0])*19*19 + quant(v[1])*19 + quant(v[2])
}

func signPow(v, exp float64) float64 {
	r := math.Pow(math.Abs(v), exp)
	if v < 0 {
		return -r
	}
	return r
}

func srgbToLinear(value int) float64 {
	v := float64(value) / 255
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func linearToSrgb(value float64) int {
	v := math.Max(0, math.Min(1, value))
	if v <= 0.0031308 {
		return int(v*12.92*255 + 0.5)
	}
	return int((1.055*math.Pow(v, 1/2.4)-0.055)*255 + 0.5)
}

// blurAlphabet is base 83, chosen by the format because it is URL- and
// JSON-safe without escaping.
const blurAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~"

func encode83(dst []byte, value, length int) []byte {
	for i := 1; i <= length; i++ {
		digit := (value / int(math.Pow(83, float64(length-i)))) % 83
		dst = append(dst, blurAlphabet[digit])
	}
	return dst
}
