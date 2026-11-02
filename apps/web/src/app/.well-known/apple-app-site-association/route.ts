// iOS Universal Links. APPLE_APP_ID is "<TEAM_ID>.<bundle id>"; 404 until set.
// Paths match the screens the app can open (MealsLinks in the KMM shared module).
export const dynamic = "force-dynamic";

const PATHS = ["/*/kitchens/*", "/*/subscriptions", "/*/subscriptions/*", "/*/rotation"];

export function GET() {
  const appID = process.env.APPLE_APP_ID;
  if (!appID) return new Response(null, { status: 404 });
  return Response.json({
    applinks: {
      details: [{ appIDs: [appID], components: PATHS.map((p) => ({ "/": p })) }],
    },
  });
}
