// Android App Links verification. Lists the release signing certificate(s)
// from ANDROID_CERT_SHA256 (comma-separated); 404 until it is set.
export const dynamic = "force-dynamic";

export function GET() {
  const prints = (process.env.ANDROID_CERT_SHA256 ?? "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
  if (prints.length === 0) return new Response(null, { status: 404 });
  return Response.json([
    {
      relation: ["delegate_permission/common.handle_all_urls"],
      target: {
        namespace: "android_app",
        package_name: process.env.ANDROID_PACKAGE ?? "ae.marketplace.mobile",
        sha256_cert_fingerprints: prints,
      },
    },
  ]);
}
