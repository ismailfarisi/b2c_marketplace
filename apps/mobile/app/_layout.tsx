import { Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { configureApiClient, ApiError } from "@marketplace/api-client";
import { getLocales } from "expo-localization";
import Constants from "expo-constants";
import { I18nManager } from "react-native";
import { useState } from "react";

import { tokenStore } from "@/lib/token-store";

const deviceLocale = getLocales()[0]?.languageCode === "ar" ? "ar" : "en";

/**
 * RTL is decided before the first render.
 *
 * React Native applies `forceRTL` at native layout level and the change only
 * takes effect after a reload — so this must run at module scope, not in an
 * effect. Doing it later gives Arabic users a left-to-right first launch.
 */
if (deviceLocale === "ar" && !I18nManager.isRTL) {
  I18nManager.allowRTL(true);
  I18nManager.forceRTL(true);
}

configureApiClient({
  baseUrl:
    (Constants.expoConfig?.extra?.apiUrl as string | undefined) ??
    "http://localhost:8080/v1",
  tokens: tokenStore,
  locale: () => deviceLocale,
  onUnauthenticated: () => {
    // Routed to the sign-in screen once auth screens land in Phase 1.
  },
});

export default function RootLayout() {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 30_000,
            retry: (failureCount, error) => {
              if (error instanceof ApiError && error.status < 500) return false;
              return failureCount < 2;
            },
          },
        },
      }),
  );

  return (
    <QueryClientProvider client={client}>
      <StatusBar style="auto" />
      <Stack screenOptions={{ headerShown: false }} />
    </QueryClientProvider>
  );
}
