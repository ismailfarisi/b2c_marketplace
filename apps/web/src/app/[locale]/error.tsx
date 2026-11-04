"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect } from "react";

import { CustomerShell, type Locale } from "@/components/meals/shell";

const COPY = {
  en: {
    title: "Something went wrong",
    body: "This page hit a problem. Try again, or head back to the kitchens.",
    retry: "Try again",
    home: "Back to kitchens",
    ref: "Reference",
  },
  ar: {
    title: "حدث خطأ ما",
    body: "واجهت هذه الصفحة مشكلة. حاول مرة أخرى أو عد إلى المطابخ.",
    retry: "حاول مرة أخرى",
    home: "العودة إلى المطابخ",
    ref: "المرجع",
  },
};

export default function LocaleError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  const params = useParams<{ locale?: string }>();
  const locale: Locale = params?.locale === "ar" ? "ar" : "en";
  const t = COPY[locale];
  useEffect(() => {
    console.error(error);
  }, [error]);
  return (
    <CustomerShell locale={locale}>
      <div className="ms-empty" role="alert">
        <h1>{t.title}</h1>
        <p>{t.body}</p>
        {error.digest ? (
          <p className="ms-note">
            {t.ref}: <span dir="ltr">{error.digest}</span>
          </p>
        ) : null}
        <div className="row">
          <button type="button" className="ms-btn" onClick={reset}>
            {t.retry}
          </button>
          <Link href={`/${locale}`} className="ms-btn">
            {t.home}
          </Link>
        </div>
      </div>
    </CustomerShell>
  );
}
