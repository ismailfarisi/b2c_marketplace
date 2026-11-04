// iOS Universal Links. APPLE_APP_ID is "<TEAM_ID>.<bundle id>"; 404 until set.
// Paths match the screens the app can open (MealsLinks in the KMM shared module)
// and the Android App Link prefixes in androidApp/src/main/AndroidManifest.xml
// (/{en,ar}/kitchens/, /subscriptions, /rotation — a prefix, so it also covers
// /rotations/{id}); MealsLinksTest.appLinkPathsOpenAScreen checks each shape.
// Support, invoices and collections stay web pages: the app opens them in the
// browser, and claiming them as app links would send that straight back here.
export const dynamic = "force-dynamic";

const PATHS = [
  "/*/kitchens/*",
  "/*/subscriptions",
  "/*/subscriptions/*",
  "/*/rotation",
  "/*/rotations",
  "/*/rotations/*",
];

export function GET() {
  const appID = process.env.APPLE_APP_ID;
  if (!appID) return new Response(null, { status: 404 });
  return Response.json({
    applinks: {
      details: [{ appIDs: [appID], components: PATHS.map((p) => ({ "/": p })) }],
    },
  });
}
