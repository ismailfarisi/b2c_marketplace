import { apiFetch } from "@marketplace/api-client";

import type { Address } from "./customer-api";

/** The signed-in account: profile, default address and marketing switches. */

export interface Profile {
  id: string;
  phone: string;
  email: string | null;
  display_name: string | null;
  locale: "en" | "ar";
  status: string;
  created_at: string;
}

export interface ProfileInput {
  display_name?: string;
  email?: string;
  locale?: "en" | "ar";
}

export const getProfile = () => apiFetch<Profile>({ url: "/me", method: "GET" });

export const updateProfile = (data: ProfileInput) =>
  apiFetch<Profile>({ url: "/me", method: "PATCH", data });

export const setDefaultAddress = (id: string) =>
  apiFetch<Address>({
    url: `/me/addresses/${id}`,
    method: "PATCH",
    data: { is_default: true },
  });

export interface NotificationPreference {
  category: string;
  channel: string;
  allowed: boolean;
  optional: boolean;
}

export const listNotificationPreferences = () =>
  apiFetch<{ items: NotificationPreference[] }>({
    url: "/me/notification-preferences",
    method: "GET",
  });

export const setNotificationPreference = (data: {
  category: string;
  channel: string;
  allowed: boolean;
}) =>
  apiFetch<void>({ url: "/me/notification-preferences", method: "PUT", data });
