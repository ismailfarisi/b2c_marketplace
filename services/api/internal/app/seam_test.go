package app

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Phase 6 gate, mechanised.
//
// PLAN-VIDEO-FIRST §2 makes a claim about the action seam: "the feed never
// learns what a car is." Phase 5 checked the mirror claim about the fulfillment
// seam by counting the lines it took to add meals. That worked because the
// change was small enough to eyeball; it does not work here, because the way
// this seam leaks is not a big diff — it is one import added eighteen months
// from now by someone who needs a listing's price on a feed tile and reaches
// for the obvious package.
//
// So the check is a test rather than a paragraph in a document. It fails on the
// import, in CI, before the leak has anything built on top of it.

// core is what must not know the verticals exist.
//
// The upstream side of the seam. Everything here serves any vertical or none,
// and the moment one of them imports a specific one, "adding a vertical means
// registering a handler" stops being true — the next vertical costs a change to
// the feed instead.
var core = []string{
	// Phase 7 — the downstream side of the same argument.
	//
	// PLAN.md §1 claims dispatch, routing and the driver app read the
	// Fulfillment table and nothing else. `dispatch` and `driver` are where that
	// is either true or it is not, and they are guarded here for the same reason
	// the feed packages are: the leak is one import, added by someone who needs
	// an order total on a driver's screen.
	//
	// `console` is deliberately absent. It answers "what does my kitchen cook by
	// six", which is meaningless without knowing what a meal is — the seam is a
	// rule about questions, not about layers. See the package comment there.
	"modules/dispatch",
	"modules/driver",

	"modules/feed",
	"modules/ranking",
	"modules/moderation",
	"modules/reels",
	"modules/media",
	"modules/comments",
	"modules/interests",
	"modules/engagement",
	"modules/social",
	"modules/search",
	"modules/actions",
	"modules/attribution",
	"seam/action",
	"seam/fulfillment",
	"seam/servicetype",
}

// verticals is what they must not import.
//
// `catalog` is deliberately absent: it owns the shared CTA pairs and is part of
// the engine. `classifieds`, `chat` and `trust` are here alongside `vehicles`
// because they arrived with it — a feed that imported the fraud scorer to grey
// out a listing would have leaked exactly as badly as one that imported the
// listings table.
var verticals = []string{
	"internal/verticals/",
	"internal/modules/vehicles",
	"internal/modules/classifieds",
	"internal/modules/chat",
	"internal/modules/trust",
	"internal/modules/subscriptions",
	// Phase 7. `ordering` and `catalog` are what a driver's screen would reach
	// for if the primitive did not hold — an order total, an item list, a
	// service type — so they are named here even though they are engine rather
	// than vertical.
	"internal/modules/ordering",
	"internal/modules/catalog",
	"internal/modules/console",
	// Phase 9. `boosts` is not a vertical — it serves any of them — but the
	// feed must not import it, and for a reason the other entries do not share.
	// The claim the whole ad design rests on is that a sponsored reel passes
	// the same hard filters as an organic one. That is enforceable only while
	// the boosts query applies `resolve_reel_coverage` itself; the moment the
	// feed can call into the ad system directly, someone passes it a candidate
	// set that has already skipped the geo check, and the guarantee becomes a
	// paragraph in a document.
	"internal/modules/boosts",
}

func TestCoreDoesNotImportAnyVertical(t *testing.T) {
	for _, pkg := range core {
		dir := filepath.Join("..", filepath.FromSlash(pkg))
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("%s does not exist — this test is guarding a package that moved", pkg)
		}

		for _, imp := range importsOf(t, dir) {
			for _, forbidden := range verticals {
				if strings.Contains(imp, forbidden) {
					t.Errorf("%s imports %s\n\n"+
						"The action seam has leaked. Everything in core serves any vertical\n"+
						"or none; if it needs something from one, the vertical should be\n"+
						"handing it over through the seam (an action handler, or an\n"+
						"interface the core declares — see reels.SubjectAuthorizer).",
						pkg, imp)
				}
			}
		}
	}
}

// TestOnlyTheWiringLayerKnowsEveryVertical is the positive half.
//
// A test that only forbids things can be satisfied by deleting the feature. This
// one asserts that `app` — the package whose entire job is to know the whole
// graph — does import the verticals, so that a future refactor which "fixes"
// the test above by moving registration into each module gets caught here.
func TestOnlyTheWiringLayerKnowsEveryVertical(t *testing.T) {
	imports := strings.Join(importsOf(t, "."), " ")
	for _, want := range []string{
		"internal/modules/vehicles",
		"internal/verticals/meals",
		"internal/verticals/laundry",
	} {
		if !strings.Contains(imports, want) {
			t.Errorf("app does not import %s — registration has moved out of the wiring layer", want)
		}
	}
}

func importsOf(t *testing.T, dir string) []string {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		// Test files are excluded. A test importing a vertical to build a
		// fixture is not a seam leak — the production dependency graph is what
		// this is about.
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}

	var out []string
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, imp := range file.Imports {
				out = append(out, strings.Trim(imp.Path.Value, `"`))
			}
		}
	}
	return out
}
