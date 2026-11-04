"use client";

import Link from "next/link";
import { useParams } from "next/navigation";

import { CustomerShell, type Locale } from "@/components/meals/shell";

const COPY = {
  en: {
    title: "We couldn't find that page",
    body: "The link may be old, or the page may have moved.",
    home: "Back to kitchens",
  },
  ar: {
    title: "لم نعثر على هذه الصفحة",
    body: "ربما الرابط قديم، أو تم نقل الصفحة.",
    home: "العودة إلى المطابخ",
  },
};

export default function NotFound() {
  const params = useParams<{ locale?: string }>();
  const locale: Locale = params?.locale === "ar" ? "ar" : "en";
  const t = COPY[locale];
  return (
    <CustomerShell locale={locale}>
      <div className="ms-empty">
        <h1>{t.title}</h1>
        <p>{t.body}</p>
        <Link href={`/${locale}`} className="ms-btn">
          {t.home}
        </Link>
      </div>
    </CustomerShell>
  );
}
