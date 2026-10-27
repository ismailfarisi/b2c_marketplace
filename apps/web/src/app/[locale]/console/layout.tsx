import { use, type ReactNode } from "react";

import { VendorGate } from "@/components/vendor/portal";
import "@/app/admin.css";

/**
 * The vendor portal.
 *
 * Shares the staff console's stylesheet and English chrome. Content a kitchen
 * writes — dish names, descriptions — is often Arabic, and every such element
 * carries `dir="auto"` so it reads correctly inside the English frame.
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
    <div dir="ltr">
      <VendorGate locale={locale}>{children}</VendorGate>
    </div>
  );
}
