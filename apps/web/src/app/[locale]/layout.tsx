import type { Metadata } from "next";
import type { ReactNode } from "react";

import { Providers } from "@/app/providers";
import "@/app/globals.css";

export const metadata: Metadata = {
  title: "Marketplace",
  description: "Discover local food, services and vehicles",
};

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
    <html lang={locale} dir={dir}>
      <body>
        <Providers locale={locale === "ar" ? "ar" : "en"}>{children}</Providers>
      </body>
    </html>
  );
}
