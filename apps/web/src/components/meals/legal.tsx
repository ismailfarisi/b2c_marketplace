"use client";

import { CustomerShell, type Locale } from "@/components/meals/shell";

// Filled in before launch: the licensed entity and the address people write to.
export const LEGAL = {
  entity: process.env.NEXT_PUBLIC_LEGAL_ENTITY ?? "[Company legal name]",
  email: process.env.NEXT_PUBLIC_LEGAL_EMAIL ?? "[privacy@your-domain]",
  updated: "2026-11-02",
};

export type LegalDoc = Record<
  Locale,
  { title: string; updated: string; sections: { h: string; p: string[] }[] }
>;

export function LegalPage({ locale, doc }: { locale: Locale; doc: LegalDoc }) {
  const d = doc[locale];
  return (
    <CustomerShell locale={locale}>
      <article className="ms-legal">
        <h1>{d.title}</h1>
        <p className="ms-sub">
          {d.updated} {LEGAL.updated}
        </p>
        {d.sections.map((s) => (
          <section key={s.h}>
            <h2>{s.h}</h2>
            {s.p.map((p) => (
              <p key={p}>{p}</p>
            ))}
          </section>
        ))}
      </article>
    </CustomerShell>
  );
}
