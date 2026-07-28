"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { configureApiClient, ApiError } from "@marketplace/api-client";
import { useState, type ReactNode } from "react";

configureApiClient({
  baseUrl: process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080/v1",
  locale: () => document.documentElement.lang || "en",
});

export function Providers({ children, locale }: { children: ReactNode; locale: "en" | "ar" }) {
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
