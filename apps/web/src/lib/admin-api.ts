import { apiFetch } from "@marketplace/api-client";

/**
 * Bindings for the staff surface.
 *
 * Split from `api.ts` rather than appended to it, and the reason is not file
 * size. Everything here requires a `platform_admins` row, and keeping it in one
 * module means the admin bundle is one import away from being code-split out of
 * the customer app entirely — which matters because a customer's browser should
 * not be shipped the shape of the moderation queue.
 *
 * Hand-written, like `api.ts`, because `make gen-ts` has never run. Every type
 * is a copy of a schema in api/openapi.yaml and can therefore drift; the
 * mitigation is that it is one short file.
 */

// ── the overview ────────────────────────────────────────────────────────────

export interface QueueDepth {
  queue: string;
  waiting: number;
  oldest_seconds: number;
  /** The promise this queue makes, carried from the server so the client
   *  renders the deadline the system actually keeps rather than one a designer
   *  picked. */
  sla_seconds: number;
  sla_breached: boolean;
}

export const getQueues = () =>
  apiFetch<{ queues: QueueDepth[] }>({ url: "/moderation/queues", method: "GET" });

// ── reels ───────────────────────────────────────────────────────────────────

export interface ReelQueueItem {
  reel_id: string;
  author_type: string;
  author_id: string;
  caption: string;
  poster_key: string;
  categories: string[] | null;
  tier: string;
  reports: number;
  entered_at: string;
  waiting_seconds: number;
  sla_breached: boolean;
}

export const getReelQueue = () =>
  apiFetch<{ items: ReelQueueItem[] }>({ url: "/moderation/queue", method: "GET" });

export const decideReel = (reelId: string, decision: "approve" | "reject", notes: string) =>
  apiFetch<{ status: string }>({
    url: `/moderation/${reelId}/decide`,
    method: "POST",
    data: { decision, notes },
  });

// ── comments ────────────────────────────────────────────────────────────────

export interface CommentQueueItem {
  id: string;
  reel_id: string;
  author_id: string;
  author_name: string;
  body: string;
  state: string;
  reports: number;
  created_at: string;
}

export const getCommentQueue = () =>
  apiFetch<{ items: CommentQueueItem[] }>({ url: "/moderation/comments", method: "GET" });

export const decideComment = (
  commentId: string,
  decision: "approve" | "reject",
  notes: string,
) =>
  apiFetch<{ status: string }>({
    url: `/moderation/comments/${commentId}/decide`,
    method: "POST",
    data: { decision, notes },
  });

// ── chat messages ───────────────────────────────────────────────────────────

export interface MessageQueueItem {
  id: string;
  conversation_id: string;
  listing_id: string | null;
  sender_id: string;
  body: string;
  /** What the scanner found — a phone number shape, advance-fee language. The
   *  reviewer decides *with* this, not against it. */
  findings: string[] | null;
  reason: string;
  created_at: string;
}

export const getMessageQueue = () =>
  apiFetch<{ items: MessageQueueItem[] }>({ url: "/moderation/messages", method: "GET" });

export const decideMessage = (messageId: string, release: boolean) =>
  apiFetch<{ status: string }>({
    url: `/moderation/messages/${messageId}/decide`,
    method: "POST",
    data: { release },
  });

// ── listings ────────────────────────────────────────────────────────────────

export interface Money {
  amount: number;
  currency: string;
}

export interface ListingQueueItem {
  id: string;
  title: string;
  seller_kind: string;
  seller_id: string;
  seller_name: string;
  make: string;
  model: string;
  year: number;
  price: Money;
  mileage_km: number;
  vin: string | null;
  regional_spec: string;
  city: string | null;
  photo_key: string | null;
  submitted_at: string;
  trust: { score: number; band: string };
}

export const getListingQueue = () =>
  apiFetch<{ items: ListingQueueItem[] }>({ url: "/moderation/listings", method: "GET" });

export const decideListing = (listingId: string, approve: boolean, reason: string) =>
  apiFetch<{ status: string }>({
    url: `/moderation/listings/${listingId}/decide`,
    method: "POST",
    data: { approve, reason },
  });

export interface FraudSignal {
  subject_type: string;
  subject_id: string;
  kind: string;
  severity: string;
  score: number;
  band: string;
  weight_if_confirmed: number;
  created_at: string;
}

export const getFraudQueue = () =>
  apiFetch<{ items: FraudSignal[] }>({ url: "/moderation/fraud", method: "GET" });

export const decideFraud = (body: {
  subject_type: string;
  subject_id: string;
  kind: string;
  confirm: boolean;
}) => apiFetch<{ status: string }>({ url: "/moderation/fraud/decide", method: "POST", data: body });

// ── appeals ─────────────────────────────────────────────────────────────────

export interface OverdueAppeal {
  id: string;
  subject_type: string;
  subject_id: string;
  appellant_id: string;
  created_at: string;
  due_at: string;
  overdue_by_hours: number;
}

export const getAppealQueue = () =>
  apiFetch<{ items: OverdueAppeal[] }>({ url: "/moderation/appeals", method: "GET" });

export const decideAppeal = (appealId: string, overturn: boolean, note: string) =>
  apiFetch<void>({
    url: `/moderation/appeals/${appealId}/decide`,
    method: "POST",
    data: { overturn, note },
  });

// ── KYC ─────────────────────────────────────────────────────────────────────

export interface KycDocument {
  id: string;
  kind: string;
  document_ref: string | null;
  number_last4: string | null;
  issued_on: string | null;
  expires_on: string | null;
  status: string;
  /** Computed server-side. Approving a case backed by a licence that expired
   *  last week is the failure this flag exists to make impossible to miss. */
  expired: boolean;
}

export interface KycCase {
  id: string;
  subject_type: string;
  subject_id: string;
  subject_name: string;
  tier: string;
  status: string;
  created_at: string;
  documents: KycDocument[];
}

export const getKycQueue = () =>
  apiFetch<{ items: KycCase[] }>({ url: "/kyc/cases", method: "GET" });

export const decideKyc = (caseId: string, approve: boolean, reason: string) =>
  apiFetch<void>({
    url: `/kyc/cases/${caseId}/decide`,
    method: "POST",
    data: { approve, reason },
  });

export interface PendingPayoutAccount {
  id: string;
  owner_type: string;
  owner_id: string;
  /** The legal name on record and the name on the bank account. Both are
   *  returned uncompared — deciding whether they are the same entity is the
   *  reviewer's entire job, and "Karama Kitchen L.L.C." against "KARAMA KITCHEN
   *  LLC" is not a regex. */
  owner_name: string;
  holder_name: string;
  iban: string;
  bank_name: string | null;
  currency: string;
  created_at: string;
}

export const getPayoutAccountQueue = () =>
  apiFetch<{ items: PendingPayoutAccount[] }>({ url: "/payout-accounts", method: "GET" });

export const verifyPayoutAccount = (accountId: string, approve: boolean) =>
  apiFetch<void>({
    url: `/payout-accounts/${accountId}/verify`,
    method: "POST",
    data: { approve },
  });

// ── campaigns ───────────────────────────────────────────────────────────────

export interface PendingCampaign {
  id: string;
  reference: string;
  advertiser_type: string;
  advertiser_name: string;
  reel_id: string;
  caption: string;
  poster_key: string;
  bid_cpm: Money;
  funded: Money;
  daily_cap: Money;
  starts_on: string;
  created_at: string;
  /** Rendered as a sentence server-side. The reviewer's question is "is this
   *  sane", and eight nullable columns is not the shape that answers it. */
  targeting: string;
}

export const getCampaignQueue = () =>
  apiFetch<{ items: PendingCampaign[] }>({ url: "/moderation/boosts", method: "GET" });

export const reviewCampaign = (campaignId: string, approve: boolean, note: string) =>
  apiFetch<void>({
    url: `/moderation/boosts/${campaignId}/decide`,
    method: "POST",
    data: { approve, note },
  });

// ── disputes ────────────────────────────────────────────────────────────────

export interface Dispute {
  id: string;
  reference_no: string;
  subject_type: string;
  subject_id: string;
  kind: string;
  state: string;
  due_at: string | null;
  assigned_to: string | null;
  created_at: string;
  overdue: boolean;
}

export const getDisputeQueue = () =>
  apiFetch<{ items: Dispute[] }>({ url: "/disputes", method: "GET" });

export const resolveDispute = (
  disputeId: string,
  body: { resolution: string; amount_minor?: number | undefined; note: string },
) =>
  apiFetch<{ status: string }>({
    url: `/disputes/${disputeId}/resolve`,
    method: "POST",
    data: body,
  });

// ── analytics ───────────────────────────────────────────────────────────────

export const getPlatformFunnel = () =>
  apiFetch<Record<string, unknown>>({ url: "/analytics/feed-funnel", method: "GET" });

export const getRankingProfile = () =>
  apiFetch<Record<string, unknown>>({ url: "/ranking/profile", method: "GET" });
