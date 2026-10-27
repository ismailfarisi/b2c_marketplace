"use client";

import Link from "next/link";
import { useEffect, useState, type ReactNode } from "react";

import { logout } from "@/lib/auth-api";
import { tokenStore } from "@/lib/token-store";

const COPY = {
  en: {
    home: "Kitchens",
    mine: "My subscriptions",
    signIn: "Sign in",
    signOut: "Sign out",
  },
  ar: {
    home: "المطابخ",
    mine: "اشتراكاتي",
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
  return (
    <div className="ms">
      <header className="ms-bar">
        <Link href={back?.href ?? `/${locale}`} className="ms-brand">
          {back ? `← ${back.label}` : t.home}
        </Link>
        <nav className="ms-nav">
          <Link href={`/${locale}/subscriptions`}>{t.mine}</Link>
          {signedIn ? (
            <button
              type="button"
              className="ms-link"
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
      <main className="ms-main">{children}</main>
    </div>
  );
}
