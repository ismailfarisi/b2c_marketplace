import { apiFetch } from "@marketplace/api-client";

/**
 * Hand-written bindings for the Phase 7 surface.
 *
 * These exist because `make gen-ts` has never been run — there is no
 * `src/gen/` — and writing them by hand is the honest interim rather than
 * pretending the generated client exists. Every type here is a *copy* of a
 * schema in api/openapi.yaml, which means it can drift, and the mitigation is
 * that it is one file and it is short.
 *
 * When the generator lands these are deleted, not migrated. Nothing else in the
 * app imports `apiFetch` directly, so the blast radius of that change is this
 * file.
 *
 * Worth noting for the mobile side: the reel feed is going to Kotlin
 * Multiplatform, so this package will never be its client. That makes
 * api/openapi.yaml the actual cross-language contract rather than a description
 * of one — see docs/PHASE-7-CLIENTS-AND-LOGISTICS.md §7.
 */

export interface Membership {
  vendor_id: string;
  role: "owner" | "manager" | "staff";
  branch_id: string | null;
}

export interface BranchSummary {
  date: string;
  scheduled: number;
  in_progress: number;
  completed: number;
  failed: number;
  unassigned: number;
  cash_held_minor: number;
  payable_minor: number;
  currency: string;
}

export interface QueueItem {
  fulfillment_id: string;
  order_no: string;
  service_type: string;
  kind: string;
  status: string;
  window: string;
  recipient_name: string;
  area: string;
  items: string;
  payload_ref: string;
  driver_name: string;
  eta: string | null;
  attempt_count: number;
}

export interface BoardRoute {
  id: string;
  driver_id: string | null;
  driver_name: string;
  status: string;
  planned_stops: number;
  completed_stops: number;
  failed_stops: number;
  planned_distance_m: number;
  first_eta: string | null;
  last_eta: string | null;
  started_at: string | null;
}

export interface DispatchBoard {
  date: string;
  routes: BoardRoute[];
  unassigned: number;
}

export interface ManifestStop {
  fulfillment_id: string;
  route_id: string;
  sequence: number;
  kind: string;
  status: string;
  address: {
    line: string;
    landmark: string;
    area: string;
    city: string;
    notes: string;
    lat: number;
    lng: number;
  };
  recipient_name: string;
  recipient_phone: string;
  window: string;
  eta: string | null;
  cod_minor: number;
  currency: string;
  attempt_count: number;
  requires_otp: boolean;
}

export interface PayoutStatement {
  id: string;
  reference_no: string;
  period_start: string;
  period_end: string;
  currency: string;
  gross_minor: number;
  commission_minor: number;
  vat_minor: number;
  adjustments_minor: number;
  net_minor: number;
  state: string;
  issued_at: string | null;
  paid_at: string | null;
}

// ── console ─────────────────────────────────────────────────────────────────

export const listMemberships = () =>
  apiFetch<{ items: Membership[] }>({ url: "/me/memberships", method: "GET" });

export const getSummary = (branchId: string, date?: string) =>
  apiFetch<BranchSummary>({
    url: `/branches/${branchId}/summary`,
    method: "GET",
    params: { date },
  });

export const getQueue = (branchId: string, date?: string) =>
  apiFetch<{ items: QueueItem[] }>({
    url: `/branches/${branchId}/queue`,
    method: "GET",
    params: { date },
  });

export const getBoard = (branchId: string, date?: string) =>
  apiFetch<DispatchBoard>({
    url: `/branches/${branchId}/dispatch`,
    method: "GET",
    params: { date },
  });

export const planRoutes = (branchId: string, date?: string) =>
  apiFetch<{
    routes_created: number;
    stops_routed: number;
    unrouted: number;
    stops_already_late: number;
  }>({ url: `/branches/${branchId}/dispatch/plan`, method: "POST", params: { date } });

export const assignRoute = (routeId: string, driverId: string) =>
  apiFetch<{ status: string }>({
    url: `/routes/${routeId}/assign`,
    method: "POST",
    data: { driver_id: driverId },
  });

export const listStatements = (vendorId: string) =>
  apiFetch<{ items: PayoutStatement[] }>({
    url: `/vendors/${vendorId}/statements`,
    method: "GET",
  });

// ── driver ──────────────────────────────────────────────────────────────────

export const startShift = (branchId?: string) =>
  apiFetch<{ shift_id: string }>({
    url: "/driver/shifts",
    method: "POST",
    data: branchId ? { branch_id: branchId } : {},
  });

export const endShift = () =>
  apiFetch<{ status: string; note: string }>({
    url: "/driver/shifts/end",
    method: "POST",
    data: {},
  });

export const getManifest = (date?: string) =>
  apiFetch<{ date: string; stops: ManifestStop[] }>({
    url: "/driver/manifest",
    method: "GET",
    params: { date },
  });

export const dispatchStop = (fulfillmentId: string) =>
  apiFetch<{ status: string; note: string }>({
    url: `/driver/stops/${fulfillmentId}/dispatch`,
    method: "POST",
    data: {},
  });

export const advanceStop = (
  fulfillmentId: string,
  status: "en_route" | "arrived",
  at?: { lat: number; lng: number },
) =>
  apiFetch<{ status: string }>({
    url: `/driver/stops/${fulfillmentId}/advance`,
    method: "POST",
    data: { status, ...at },
  });

export const completeStop = (
  fulfillmentId: string,
  // Every optional field carries `| undefined` because
  // exactOptionalPropertyTypes is on and callers build these objects with
  // conditional values. Omitting it makes `{ otp: maybeUndefined }` a type
  // error at every call site rather than at the one place it matters.
  body: {
    proof: string;
    otp?: string | undefined;
    recipient_name?: string | undefined;
    photo_key?: string | undefined;
    note?: string | undefined;
    collected_minor?: number | undefined;
    lat?: number | undefined;
    lng?: number | undefined;
  },
) =>
  apiFetch<{ status: string }>({
    url: `/driver/stops/${fulfillmentId}/complete`,
    method: "POST",
    data: body,
  });

export const failStop = (
  fulfillmentId: string,
  body: {
    outcome: string;
    note?: string | undefined;
    lat?: number | undefined;
    lng?: number | undefined;
  },
) =>
  apiFetch<{ status: string; outcome: string }>({
    url: `/driver/stops/${fulfillmentId}/fail`,
    method: "POST",
    data: body,
  });

export const reportLocation = (body: {
  lat: number;
  lng: number;
  accuracy_m?: number | undefined;
  speed_kmh?: number | undefined;
  route_id?: string | undefined;
}) => apiFetch<void>({ url: "/driver/location", method: "POST", data: body });

export const getHeldCash = () =>
  apiFetch<{ collections: number; total_minor: number; currency: string }>({
    url: "/driver/cash",
    method: "GET",
  });

export const declareRemittance = () =>
  apiFetch<{ remittance_id: string; declared_minor: number; next_step: string }>({
    url: "/driver/cash/remittances",
    method: "POST",
    data: {},
  });
