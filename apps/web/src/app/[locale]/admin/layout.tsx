import { use, type ReactNode } from "react";

import { ConsoleShell } from "@/components/admin/shell";
import "@/app/admin.css";

/**
 * The staff console.
 *
 * **English chrome, `dir="ltr"`, regardless of the locale segment.** That
 * deserves defending, because everything else in this app is bilingual by
 * construction.
 *
 * The customer surfaces are Arabic-first because customers are. This one is used
 * by a moderation team whose working language is the platform's, and whose job
 * is judging content that is *often* Arabic. Those are different requirements:
 * the chrome should be stable and familiar so attention goes to the content, and
 * the content itself renders in whatever language it was written in — every
 * caption, comment and message below carries `dir="auto"`, which is what makes
 * an Arabic body read correctly inside an English table.
 *
 * Localising the chrome would be a translation project with no user asking for
 * it. Localising the *content* was never optional and is done per element.
 *
 * The stylesheet is imported here rather than in globals.css so a customer's
 * browser never downloads it — the same argument `admin-api.ts` makes about the
 * bindings.
 */
export default function AdminLayout({
  children,
  params,
}: {
  children: ReactNode;
  params: Promise<{ locale: string }>;
}) {
  const { locale } = use(params);

  return (
    <div dir="ltr">
      <ConsoleShell locale={locale}>{children}</ConsoleShell>
    </div>
  );
}
