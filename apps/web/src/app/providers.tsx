"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { configureApiClient, ApiError } from "@marketplace/api-client";
import { useState, type ReactNode } from "react";

import { tokenStore } from "@/lib/token-store";

configureApiClient({
  baseUrl: process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080/v1",
  locale: () => document.documentElement.lang || "en",
  // Injected rather than imported by the client package. It was originally
  // injected so an Expo build could hand in a SecureStore-backed store; mobile
  // is Kotlin now and has its own, so what the seam buys today is that this
  // package has no opinion about where a token lives — which is what let the
  // browser get the weaker-but-correct answer (sessionStorage) without
  // dragging it into anyone else's bundle.
  tokens: tokenStore,
  onUnauthenticated: (reason) => {
    // A hard redirect rather than a router push. The refresh token is already
    // gone at this point, so every in-flight query is about to fail too, and
    // tearing the page down is the only state that is definitely consistent.
    if (typeof window === "undefined") return;

    // Staff land on the console's own sign-in rather than the storefront, and
    // keep where they were going. Sending a moderator whose session expired
    // mid-shift to the customer home page makes them find the console again by
    // hand, which for a 15-minute access token is several times a day.
    const path = window.location.pathname;
    const admin = /^\/(en|ar)\/admin(\/|$)/.exec(path);
    if (admin) {
      const login = `/${admin[1]}/admin/login`;
      // Already there: the sign-in page's own calls must not bounce it.
      if (path === login) return;
      window.location.href = `${login}?next=${encodeURIComponent(path)}`;
      return;
    }

    // The kitchen portal and the customer pages each have their own sign-in,
    // and both return the person to where they were.
    const portal = /^\/(en|ar)\/console(\/|$)/.exec(path);
    if (portal) {
      const login = `/${portal[1]}/console/login`;
      if (path === login) return;
      window.location.href = `${login}?next=${encodeURIComponent(path)}`;
      return;
    }
    const customer =
      /^\/(en|ar)\/(subscriptions|kitchens\/[^/]+\/subscribe)(\/|$)/.exec(path);
    if (customer) {
      window.location.href = `/${customer[1]}/signin?next=${encodeURIComponent(path)}`;
      return;
    }

    // A signed-out visitor on a public page just sees the public page.
    if (reason === "expired") window.location.href = "/";
  },
});

export function Providers({
  children,
  locale,
}: {
  children: ReactNode;
  locale: "en" | "ar";
}) {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 30_000,
            retry: (failureCount, error) => {
              // Retrying a 4xx just burns the user's battery — the request is
              // not going to start succeeding. 5xx and network errors are worth
              // one or two attempts.
              if (error instanceof ApiError && error.status < 500) return false;
              return failureCount < 2;
            },
          },
          mutations: {
            // Mutations carry an Idempotency-Key, so a retry is safe. It is
            // still left to the caller, because a retried payment authorization
            // should surface to the user rather than happen silently.
            retry: false,
          },
        },
      }),
  );

  return (
    <QueryClientProvider client={client}>
      <div data-locale={locale}>{children}</div>
    </QueryClientProvider>
  );
}
