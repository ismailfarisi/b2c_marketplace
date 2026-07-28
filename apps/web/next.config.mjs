/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,

  // The generated client ships as TypeScript source rather than a build step,
  // so Next has to compile it alongside the app.
  transpilePackages: ["@marketplace/api-client"],

  // Locale routing. Arabic is carried from Phase 0 rather than retrofitted —
  // RTL affects layout, not just strings, and adding it late means re-auditing
  // every component that was ever built assuming left-to-right.
  i18n: undefined, // App Router handles this via [locale] segments, not next.config

  images: {
    // CloudFront in front of S3 (me-central-1). Data residency is a GCC
    // requirement, not a latency preference.
    remotePatterns: [
      { protocol: "https", hostname: "*.cloudfront.net" },
      { protocol: "http", hostname: "localhost", port: "9000" },
    ],
  },

  async headers() {
    return [
      {
        source: "/:path*",
        headers: [
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
          { key: "X-Frame-Options", value: "DENY" },
        ],
      },
    ];
  },
};

export default nextConfig;
