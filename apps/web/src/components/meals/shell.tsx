"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";

import { AccountDeletedNotice } from "@/components/meals/account";
import { useCustomerPoint } from "@/components/meals/location";
import { LocationPicker, placeLabel } from "@/components/meals/location-picker";
import { logout } from "@/lib/auth-api";
import { rememberInvite } from "@/lib/customer-api";
import { switchLocaleHref } from "@/lib/locale-switch";
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
    businessLong: "Office lunches for your company",
    privacy: "Privacy",
    terms: "Terms",
    help: "Help",
    signIn: "Sign in",
    signOut: "Sign out",
    deliverTo: "Deliver to",
    nearYou: "your location",
    fallbackArea: "Dubai Marina",
    search: "Search kitchens or dishes",
  },
  ar: {
    brand: "وجبات",
    feed: "فيديو",
    other: "English",
    home: "المطابخ",
    mine: "اشتراكاتي",
    mineShort: "اشتراكاتي",
    business: "غداء المكاتب",
    businessLong: "غداء المكاتب لشركتك",
    privacy: "الخصوصية",
    terms: "الشروط",
    help: "المساعدة",
    signIn: "تسجيل الدخول",
    signOut: "تسجيل الخروج",
    deliverTo: "التوصيل إلى",
    nearYou: "موقعك",
    fallbackArea: "دبي مارينا",
    search: "ابحث عن مطبخ أو طبق",
  },
};

export type Locale = "en" | "ar";

/** Header search on the home page; the kitchen list listens for it. */
export const SEARCH_EVENT = "meals-search";

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
  const router = useRouter();
  const { place, precise, choose, locate } = useCustomerPoint();
  const placeName = place
    ? placeLabel(place, locale)
    : precise
      ? t.nearYou
      : t.fallbackArea;
  const onHome = path === `/${locale}`;
  const [q, setQ] = useState("");
  useEffect(() => {
    setQ(new URLSearchParams(window.location.search).get("q") ?? "");
  }, []);
  // Home filters live as you type; elsewhere a search opens home with ?q=.
  const search = (value: string, submit: boolean) => {
    if (onHome) {
      window.history.replaceState(
        null,
        "",
        value ? `?q=${encodeURIComponent(value)}` : window.location.pathname,
      );
      window.dispatchEvent(new CustomEvent(SEARCH_EVENT, { detail: value }));
    } else if (submit) {
      router.push(
        `/${locale}${value ? `?q=${encodeURIComponent(value)}` : ""}`,
      );
    }
  };
  return (
    <div className="ms">
      <header className="ms-bar">
        <Link href={`/${locale}`} className="ms-brand">
          {t.brand}
        </Link>
        <div className="ms-bar-find">
          <LocationPicker
            className="ms-bar-place"
            locale={locale}
            current={place}
            onChoose={choose}
            onLocate={locate}
            label={
              <>
                <span className="ms-bar-place-k">{t.deliverTo}</span>{" "}
                <strong>{placeName}</strong>
              </>
            }
          />
          <form
            role="search"
            className="ms-bar-search"
            action={`/${locale}`}
            onSubmit={(e) => {
              e.preventDefault();
              search(q, true);
            }}
          >
            <input
              type="search"
              name="q"
              value={q}
              onChange={(e) => {
                setQ(e.target.value);
                search(e.target.value, false);
              }}
              placeholder={t.search}
              aria-label={t.search}
            />
          </form>
        </div>
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
          <Link
            href={switchHref}
            lang={otherLocale}
            hrefLang={otherLocale}
            onClick={(e) => {
              // Read at click time: the query string (?next=, ?q=, ?ref=)
              // must survive the switch.
              e.preventDefault();
              router.push(
                switchLocaleHref(path, window.location.search, otherLocale),
              );
            }}
          >
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
        <AccountDeletedNotice locale={locale} />
        {back && (
          <Link href={back.href} className="ms-back">
            <span aria-hidden>{locale === "ar" ? "→" : "←"}</span> {back.label}
          </Link>
        )}
        {children}
      </main>
      <footer className="ms-foot">
        <Link href={`/${locale}/business`}>{t.businessLong}</Link>
        <Link href={`/${locale}/feed`}>{t.feed}</Link>
        <Link href={`/${locale}/support`}>{t.help}</Link>
        <Link href={`/${locale}/privacy`}>{t.privacy}</Link>
        <Link href={`/${locale}/terms`}>{t.terms}</Link>
      </footer>
    </div>
  );
}
