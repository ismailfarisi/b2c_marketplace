import { use, type ReactNode } from "react";

import { VendorShell } from "@/components/kitchen/portal";

export default function BranchLayout({
  children,
  params,
}: {
  children: ReactNode;
  params: Promise<{ locale: string; branchId: string }>;
}) {
  const { locale, branchId } = use(params);
  return (
    <VendorShell locale={locale} branchId={branchId}>
      {children}
    </VendorShell>
  );
}
