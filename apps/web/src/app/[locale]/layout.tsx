import type { Metadata } from "next";
import {
  Bricolage_Grotesque,
  DM_Sans,
  IBM_Plex_Sans_Arabic,
} from "next/font/google";
import type { ReactNode } from "react";

import { Providers } from "@/app/providers";
import "@/app/globals.css";

export const metadata: Metadata = {
  title: "Meals: home-style kitchens on subscription",
  description:
    "Subscribe to a home-style kitchen near you: a weekday menu, delivered to your home or office.",
};

const display = Bricolage_Grotesque({
  subsets: ["latin"],
  weight: ["500", "700", "800"],
  variable: "--font-display",
  display: "swap",
});
const body = DM_Sans({
  subsets: ["latin"],
  weight: ["400", "500", "600", "700"],
  variable: "--font-body",
  display: "swap",
});
const arabic = IBM_Plex_Sans_Arabic({
  subsets: ["arabic"],
  weight: ["400", "500", "600", "700"],
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
