import type { Metadata } from "next";
import localFont from "next/font/local";
import type { ReactNode } from "react";

import { Providers } from "@/app/providers";
import "@/app/globals.css";

export const metadata: Metadata = {
  title: "Meals: home-style kitchens on subscription",
  description:
    "Subscribe to a home-style kitchen near you: a weekday menu, delivered to your home or office.",
};

// Self-hosted (SIL OFL, see fonts/OFL.txt) so builds never depend on fetching Google Fonts.
const display = localFont({
  src: "../fonts/bricolage-grotesque-latin-wght-normal.woff2",
  weight: "200 800",
  variable: "--font-display",
  display: "swap",
});
const body = localFont({
  src: "../fonts/dm-sans-latin-wght-normal.woff2",
  weight: "100 1000",
  variable: "--font-body",
  display: "swap",
});
const arabic = localFont({
  src: [
    {
      path: "../fonts/ibm-plex-sans-arabic-arabic-400-normal.woff2",
      weight: "400",
    },
    {
      path: "../fonts/ibm-plex-sans-arabic-arabic-500-normal.woff2",
      weight: "500",
    },
    {
      path: "../fonts/ibm-plex-sans-arabic-arabic-600-normal.woff2",
      weight: "600",
    },
    {
      path: "../fonts/ibm-plex-sans-arabic-arabic-700-normal.woff2",
      weight: "700",
    },
  ],
  variable: "--font-arabic",
  display: "swap",
});

export function generateStaticParams() {
  return [{ locale: "en" }, { locale: "ar" }];
}

/**
 * Locale is a route segment, and `dir` is set from it on the html element.
 *
 * Doing this at the root means every downstream component inherits direction
 * from CSS logical properties instead of hard-coded left/right. Retrofitting
 * that later means auditing every margin, every icon rotation and every
 * animation in the app — which is the actual reason RTL projects get
 * indefinitely postponed.
 */
export default async function LocaleLayout({
  children,
  params,
}: {
  children: ReactNode;
  params: Promise<{ locale: string }>;
}) {
  const { locale } = await params;
  const dir = locale === "ar" ? "rtl" : "ltr";

  return (
    <html
      lang={locale}
      dir={dir}
      className={`${display.variable} ${body.variable} ${arabic.variable}`}
    >
      <body>
        <Providers locale={locale === "ar" ? "ar" : "en"}>{children}</Providers>
      </body>
    </html>
  );
}
