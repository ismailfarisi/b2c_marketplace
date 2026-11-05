import { apiFetch } from "@marketplace/api-client";

/**
 * Buyer ↔ seller messaging for listings (cars, property). The conversation is
 * opened by `POST /listings/{id}/inquire` — idempotent per (listing, buyer), so a
 * second tap returns the same conversation — and then read and written through
 * `/conversations/{id}/messages`. The seller's phone number is never shown;
 * that is why chat exists at all.
 */

export interface Inquiry {
  lead_id: string;
  conversation_id: string;
}

export type MessageState = "sent" | "redacted" | "held" | "rejected";

export interface ChatMessage {
  id: string;
  sender_id: string;
  mine: boolean;
  body: string;
  state: MessageState;
  created_at: string;
}

export interface SentMessage extends ChatMessage {
  /** Present when something was removed or held; shown to the sender. */
  message?: string;
}

export const inquireAboutListing = (
  listingId: string,
  source: { reelId?: string | null; actionId?: string | null },
) =>
  apiFetch<Inquiry>({
    url: `/listings/${listingId}/inquire`,
    method: "POST",
    data: {
      source: source.reelId ? "reel_action" : "listing_page",
      ...(source.reelId ? { source_reel_id: source.reelId } : {}),
      ...(source.actionId ? { source_action_id: source.actionId } : {}),
    },
  });

export const listMessages = (conversationId: string) =>
  apiFetch<{ items: ChatMessage[] }>({
    url: `/conversations/${conversationId}/messages`,
    method: "GET",
  });

export const sendMessage = (conversationId: string, body: string) =>
  apiFetch<SentMessage>({
    url: `/conversations/${conversationId}/messages`,
    method: "POST",
    data: { body },
  });

export const markConversationRead = (conversationId: string) =>
  apiFetch<void>({
    url: `/conversations/${conversationId}/read`,
    method: "POST",
  });
