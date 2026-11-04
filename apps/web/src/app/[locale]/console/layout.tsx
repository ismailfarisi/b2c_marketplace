import { use, type ReactNode } from "react";

import { VendorGate } from "@/components/kitchen/portal";
import "@/app/admin.css";
import "@/components/kitchen/portal.css";

/**
 * The vendor portal.
 *
 * Shares the staff console's stylesheet. The chrome follows the route locale
 * (English or Arabic, RTL for Arabic); content a kitchen writes — dish names,
 * descriptions — carries `dir="auto"` so it reads correctly in either frame.
 */
export default function VendorLayout({
  children,
  params,
}: {
  children: ReactNode;
  params: Promise<{ locale: string }>;
}) {
  const { locale } = use(params);
  return (
    <div dir={locale === "ar" ? "rtl" : "ltr"} lang={locale === "ar" ? "ar" : "en"}>
      <VendorGate locale={locale}>{children}</VendorGate>
    </div>
  );
}
