import { NextResponse, type NextRequest } from "next/server";

const LOCALES = ["en", "ar"] as const;
const DEFAULT_LOCALE = "en";

/**
 * Redirects `/` to a locale-prefixed route.
 *
 * Every page lives under `/[locale]/…` so direction is decided before the first
 * byte of layout renders. A client-side locale switch would flash LTR content
 * to an Arabic user on every cold load.
 */
export function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl;

  const hasLocale = LOCALES.some(
    (l) => pathname === `/${l}` || pathname.startsWith(`/${l}/`),
  );
  if (hasLocale) return NextResponse.next();

  const header = request.headers.get("accept-language") ?? "";
  const locale = header.toLowerCase().startsWith("ar") ? "ar" : DEFAULT_LOCALE;

  const url = request.nextUrl.clone();
  url.pathname = `/${locale}${pathname}`;
  return NextResponse.redirect(url);
}

export const config = {
  matcher: ["/((?!api|_next|.*\\..*).*)"],
};
