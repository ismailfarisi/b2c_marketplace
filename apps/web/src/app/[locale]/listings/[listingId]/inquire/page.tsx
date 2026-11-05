"use client";

import { ApiError } from "@marketplace/api-client";
import { useMutation } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useEffect, useRef } from "react";

import { LoadError } from "@/components/load-error";
import { CustomerShell, asLocale, signInHref } from "@/components/meals/shell";
import { CHAT_COPY } from "@/components/chat-copy";
import { inquireAboutListing } from "@/lib/chat-api";
import { tokenStore } from "@/lib/token-store";

/**
 * Where a feed "Message seller" CTA lands on the web.
 *
 * Opens (or re-opens — the call is idempotent per listing and buyer) the
 * conversation and replaces this page with the thread. A signed-out visitor is
 * sent to sign in first and comes back here, because the inquiry is a lead
 * attributed to an account.
 */
export default function InquirePage({
  params,
}: {
  params: Promise<{ locale: string; listingId: string }>;
}) {
  const p = use(params);
  const locale = asLocale(p.locale);
  const t = CHAT_COPY[locale];
  const router = useRouter();
  const started = useRef(false);

  const inquire = useMutation({
    mutationFn: () => {
      const q = new URLSearchParams(window.location.search);
      return inquireAboutListing(p.listingId, {
        reelId: q.get("reel"),
        actionId: q.get("action"),
      });
    },
    onSuccess: (r) =>
      router.replace(`/${locale}/messages/${r.conversation_id}`),
  });

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    if (!tokenStore.getAccessToken() && !tokenStore.getRefreshToken()) {
      router.replace(
        signInHref(locale, window.location.pathname + window.location.search),
      );
      return;
    }
    inquire.mutate();
  }, [inquire, locale, router]);

  const err = inquire.error;
  const unavailable =
    err instanceof ApiError && (err.status === 409 || err.status === 404);

  return (
    <CustomerShell
      locale={locale}
      back={{ href: `/${locale}/feed`, label: t.backToFeed }}
    >
      <div className="ms-support">
        <h1>{t.inquireTitle}</h1>
        {unavailable ? (
          <p className="ms-empty" role="alert">
            {t.listingUnavailable}{" "}
            <Link href={`/${locale}/feed`}>{t.backToFeed}</Link>
          </p>
        ) : err ? (
          <LoadError
            error={err}
            onRetry={() => inquire.mutate()}
            locale={locale}
          />
        ) : (
          <p className="ms-note" aria-busy="true" role="status">
            {t.opening}
          </p>
        )}
      </div>
    </CustomerShell>
  );
}
