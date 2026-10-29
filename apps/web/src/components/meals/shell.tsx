"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";

import { logout } from "@/lib/auth-api";
import { rememberInvite } from "@/lib/customer-api";
import { tokenStore } from "@/lib/token-store";

const COPY = {
  en: {
    brand: "Meals",
    feed: "Videos",
    other: "العربية",
    home: "Kitchens",
    mine: "My subscriptions",
    mineShort: "My meals",
    business: "Office lunches",
    signIn: "Sign in",
    signOut: "Sign out",
  },
  ar: {
    brand: "وجبات",
    feed: "فيديو",
    other: "English",
    home: "المطابخ",
    mine: "اشتراكاتي",
    mineShort: "اشتراكاتي",
    business: "غداء المكاتب",
    signIn: "تسجيل الدخول",
    signOut: "تسجيل الخروج",
  },
};

export type Locale = "en" | "ar";

export const STATUS = {
  en: {
    pending_payment: "Awaiting payment",
    active: "Active",
    paused: "Paused",
    past_due: "Payment overdue",
    suspended: "Suspended",
    cancelled: "Cancelled",
    completed: "Completed",
  } as Record<string, string>,
  ar: {
    pending_payment: "بانتظار الدفع",
    active: "نشط",
    paused: "موقوف مؤقتًا",
    past_due: "الدفع متأخر",
    suspended: "معلّق",
    cancelled: "ملغى",
    completed: "مكتمل",
  } as Record<string, string>,
};

/** "30 Oct 2026" from a plain YYYY-MM-DD date, without a timezone shift. */
export function formatDay(
  locale: Locale,
  iso: string,
  opts: Intl.DateTimeFormatOptions = {
    day: "numeric",
    month: "short",
    year: "numeric",
  },
) {
  const [y, m, d] = iso.split("-").map(Number);
  if (!y || !m || !d) return iso;
  return new Intl.DateTimeFormat(locale === "ar" ? "ar-AE" : "en-GB", {
    ...opts,
    timeZone: "UTC",
  }).format(new Date(Date.UTC(y, m - 1, d)));
}

/** "30 Oct – 26 Nov 2026"; the year only once when both ends share it. */
export function formatRange(locale: Locale, from: string, to: string) {
  const sameYear = from.slice(0, 4) === to.slice(0, 4);
  const start = formatDay(
    locale,
    from,
    sameYear ? { day: "numeric", month: "short" } : undefined,
  );
  return `${start} – ${formatDay(locale, to)}`;
}

export const asLocale = (l: string): Locale => (l === "ar" ? "ar" : "en");

/**
 * Same-locale paths only: `next` arrives in a URL, and a URL is something an
 * attacker can send someone.
 */
export function safeNext(locale: Locale, next: string | null): string {
  const base = `/${locale}`;
  if (
    next &&
    next.startsWith(`${base}/`) &&
    !next.startsWith("//") &&
    !next.includes("://")
  ) {
    return next;
  }
  return `${base}/subscriptions`;
}

export function signInHref(locale: Locale, next: string) {
  return `/${locale}/signin?next=${encodeURIComponent(next)}`;
}

/** Whether this tab holds a session. A hint for the header, never an authorisation. */
export function useHasSession() {
  const [has, setHas] = useState(false);
  useEffect(() => {
    setHas(
      Boolean(tokenStore.getAccessToken() || tokenStore.getRefreshToken()),
    );
  }, []);
  return has;
}

export function CustomerShell({
  locale,
  back,
  children,
}: {
  locale: Locale;
  back?: { href: string; label: string };
  children: ReactNode;
}) {
  const t = COPY[locale];
  const signedIn = useHasSession();
  // A shared invite link carries ?ref=<code>; keep it for checkout.
  useEffect(() => {
    const ref = new URLSearchParams(window.location.search).get("ref");
    if (ref) rememberInvite(ref);
  }, []);
  const path = usePathname() ?? `/${locale}`;
  const otherLocale = locale === "ar" ? "en" : "ar";
  const switchHref = path.replace(/^\/(en|ar)(?=\/|$)/, `/${otherLocale}`);
  const here = (href: string) =>
    path === href || path.startsWith(`${href}/`) ? "page" : undefined;
  return (
    <div className="ms">
      <header className="ms-bar">
        <Link href={`/${locale}`} className="ms-brand">
          {t.brand}
        </Link>
        <nav className="ms-nav">
          <Link
            href={`/${locale}`}
            className="ms-nav-wide"
            aria-current={path === `/${locale}` ? "page" : undefined}
          >
            {t.home}
          </Link>
          <Link href={`/${locale}/feed`} className="ms-nav-wide">
            {t.feed}
          </Link>
          <Link
            href={`/${locale}/business`}
            className="ms-nav-wide"
            aria-current={here(`/${locale}/business`)}
          >
            {t.business}
          </Link>
          <Link
            href={`/${locale}/subscriptions`}
            aria-current={here(`/${locale}/subscriptions`)}
          >
            <span className="ms-nav-long">{t.mine}</span>
            <span className="ms-nav-short">{t.mineShort}</span>
          </Link>
          <Link href={switchHref} lang={otherLocale} hrefLang={otherLocale}>
            {t.other}
          </Link>
          {signedIn ? (
            <button
              type="button"
              onClick={async () => {
                await logout().catch(() => undefined);
                window.location.href = `/${locale}`;
              }}
            >
              {t.signOut}
            </button>
          ) : (
            <Link href={signInHref(locale, `/${locale}/subscriptions`)}>
              {t.signIn}
            </Link>
          )}
        </nav>
      </header>
      <main className="ms-main">
        {back && (
          <Link href={back.href} className="ms-back">
            <span aria-hidden>{locale === "ar" ? "→" : "←"}</span> {back.label}
          </Link>
        )}
        {children}
      </main>
    </div>
  );
}
