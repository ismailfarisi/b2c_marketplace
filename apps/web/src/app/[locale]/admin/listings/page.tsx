"use client";

import {
  decideFraud,
  decideListing,
  getFraudQueue,
  getListingQueue,
} from "@/lib/admin-api";
import { Decide, QueueState, useQueueData } from "@/components/admin/review";

/**
 * Vehicle listings and the fraud queue, on one screen.
 *
 * Together because they are the same decision seen from two angles. A listing
 * review asks "should this car be published"; a fraud signal asks "should this
 * seller keep selling". A reviewer holding the first question who cannot see the
 * second approves a car from someone with two confirmed odometer rollbacks.
 *
 * Which is why the seller's trust score rides on every listing row rather than
 * behind a link. PHASE-6 said a listing decision needs the VIN and the price
 * history in front of it; the same argument covers the seller's record, and a
 * lookup that costs a click is a lookup nobody makes at 40 listings an hour.
 */
function money(m: { amount: number; currency: string }) {
  return `${(m.amount / 100).toLocaleString("en-AE", {
    minimumFractionDigits: 0,
    maximumFractionDigits: 2,
  })} ${m.currency}`;
}

export default function ListingQueue() {
  const listings = useQueueData("listings", getListingQueue);
  const fraud = useQueueData("fraud", getFraudQueue);

  const listingItems = listings.data?.items ?? [];
  const fraudItems = fraud.data?.items ?? [];

  return (
    <>
      <h1>Listings</h1>
      <p className="muted">
        Four-hour SLA. The VIN, the price and the seller&rsquo;s trust band are on
        the row — a decision that needs a second lookup is a decision that gets
        made without it.
      </p>

      <QueueState
        isPending={listings.isPending}
        error={listings.error}
        count={listingItems.length}
        empty="No listings awaiting review."
      />

      <ul className="stack">
        {listingItems.map((l) => (
          <li key={l.id} className="card">
            <div className="row spread">
              <strong dir="auto">
                {l.year} {l.make} {l.model}
              </strong>
              <div className="row">
                <span className="pill">{l.seller_kind}</span>
                <span className={l.trust.band === "clean" ? "pill" : "pill bad"}>
                  trust {Math.round(l.trust.score)} · {l.trust.band}
                </span>
              </div>
            </div>

            <p dir="auto">{l.title}</p>

            <div className="row">
              <span>{money(l.price)}</span>
              <span className="muted">{l.mileage_km.toLocaleString()} km</span>
              <span className="muted">{l.regional_spec} spec</span>
              {l.city && <span className="muted">{l.city}</span>}
            </div>

            <p className="mono muted">
              {/* Shown in full. A VIN is the single most checkable fact on a
                  listing and truncating it defeats the check. */}
              VIN {l.vin ?? "— none supplied"} · seller {l.seller_name || l.seller_id}
            </p>

            <Decide
              onDecide={(approve, reason) => decideListing(l.id, approve, reason)}
            />
          </li>
        ))}
      </ul>

      <h2>Fraud signals</h2>
      <p className="muted">
        Confirming a signal doubles its weight against the seller&rsquo;s score.
        No machine signal can restrict an account on its own — only a person
        confirming one can, which is what this queue is.
      </p>

      <QueueState
        isPending={fraud.isPending}
        error={fraud.error}
        count={fraudItems.length}
        empty="No open signals."
      />

      <ul className="stack tight">
        {fraudItems.map((s) => (
          <li key={`${s.subject_type}:${s.subject_id}:${s.kind}`} className="card">
            <div className="row spread">
              <div className="row">
                <span className="pill bad">{s.kind}</span>
                <span className="pill">{s.severity}</span>
              </div>
              <span className={s.band === "clean" ? "pill" : "pill bad"}>
                {Math.round(s.score)} · {s.band}
              </span>
            </div>

            <p className="muted">
              {/* The consequence, before the decision. A reviewer confirming a
                  signal should know it is worth 90 points against a threshold
                  of 25, not discover it from the seller's support ticket. */}
              Confirming costs this seller {s.weight_if_confirmed} points.
            </p>

            <p className="mono muted">
              {s.subject_type} {s.subject_id}
            </p>

            <Decide
              reasonRequired={false}
              approveLabel="Confirm"
              rejectLabel="Dismiss"
              onDecide={(approve) =>
                decideFraud({
                  subject_type: s.subject_type,
                  subject_id: s.subject_id,
                  kind: s.kind,
                  confirm: approve,
                })
              }
            />
          </li>
        ))}
      </ul>
    </>
  );
}
