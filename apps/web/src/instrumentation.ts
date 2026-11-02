import * as Sentry from "@sentry/nextjs";

// Server-side crash reporting (route handlers, server components, middleware),
// on only when SENTRY_DSN is set at runtime.
export function register() {
  const dsn = process.env.SENTRY_DSN;
  if (!dsn) return;
  Sentry.init({
    dsn,
    environment: process.env.APP_ENV ?? "development",
    sendDefaultPii: false,
    tracesSampleRate: 0,
  });
}

export const onRequestError = Sentry.captureRequestError;
