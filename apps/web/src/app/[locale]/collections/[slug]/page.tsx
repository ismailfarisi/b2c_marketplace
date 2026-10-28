"use client";

import { useQuery } from "@tanstack/react-query";
import { use } from "react";

import { COPY, KitchenCard } from "@/components/meals/kitchen-card";
import { useCustomerPoint } from "@/components/meals/location";
import { CustomerShell } from "@/components/meals/shell";
import { getMealCollection } from "@/lib/meals-api";

const GONE = {
  en: "This collection has ended or does not exist.",
  ar: "انتهت هذه المجموعة أو أنها غير موجودة.",
};

/** A merchandised collection: only the kitchens that deliver to the customer. */
export default function CollectionPage({
  params,
}: {
  params: Promise<{ locale: string; slug: string }>;
}) {
  const p = use(params);
  const locale = p.locale === "ar" ? "ar" : "en";
  const t = COPY[locale];
  const { point, precise } = useCustomerPoint();
  const c = useQuery({
    queryKey: ["meal-collection", p.slug, point?.lat, point?.lng],
    queryFn: () => getMealCollection(p.slug, point),
    enabled: point != null,
    retry: false,
  });
  const title =
    c.data && ((locale === "ar" && c.data.title_ar) || c.data.title);
  const sub =
    c.data && ((locale === "ar" && c.data.subtitle_ar) || c.data.subtitle);

  return (
    <CustomerShell locale={locale}>
      {!point || c.isPending ? (
        <p className="ms-note">{t.locating}</p>
      ) : c.isError ? (
        <p className="ms-empty">{GONE[locale]}</p>
      ) : (
        <>
          <section className="ms-home-head ms-home-solo">
            <div className="ms-intro">
              <h1 dir="auto">{title}</h1>
              {sub && <p dir="auto">{sub}</p>}
              {!precise && <p className="ms-note">{t.fallback}</p>}
            </div>
          </section>
          {c.data.kitchens.length === 0 ? (
            <p className="ms-empty">{t.empty}</p>
          ) : (
            <>
              <p className="ms-count">{t.count(c.data.kitchens.length)}</p>
              <ul className="ms-grid">
                {c.data.kitchens.map((k) => (
                  <KitchenCard key={k.branch_id} k={k} locale={locale} t={t} />
                ))}
              </ul>
            </>
          )}
        </>
      )}
    </CustomerShell>
  );
}
