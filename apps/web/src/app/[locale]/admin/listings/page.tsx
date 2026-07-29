"use client";

import {
  decideFraud,
  decideListing,
  getFraudQueue,
  getListingQueue,
} from "@/lib/admin-api";
import {
  Decide,
  Evidence,
  QueuePanel,
  Tag,
  money,
  since,
  useQueueData,
} from "@/components/admin/table";

/**
 * Vehicle listings and the fraud queue, on one screen.
 *
 * Together because they are the same decision seen from two angles. A listing
 * review asks *"should this car be published"*; a fraud signal asks *"should
 * this seller keep selling"*. A reviewer holding the first question who cannot
 * see the second approves a car from someone with two confirmed odometer
 * rollbacks.
 *
 * Which is why the seller's trust band rides on every listing row rather than
 * behind a link. PHASE-6 said a listing decision needs the VIN and the price
 * history in front of it; the same argument covers the seller's record, and a
 * lookup that costs a click is a lookup nobody makes at forty listings an hour.
 */
function trustTone(band: string) {
  if (band === "clean" || band === "ok") return undefined;
  if (band === "watch") return "warn" as const;
  return "danger" as const;
}

export default function ListingQueue() {
  const listings = useQueueData("listings", getListingQueue);
  const fraud = useQueueData("fraud", getFraudQueue);

  return (
    <>
      <p className="console-lede">
        Four-hour SLA on listings. The VIN, the price and the seller&rsquo;s trust
        band are on the row — a decision that needs a second lookup is a decision
        that gets made without it.
      </p>

      <QueuePanel
        title="Listings awaiting review"
        columns={[
          { label: "Vehicle" },
          { label: "Title", className: "grow" },
          { label: "Price", className: "num" },
          { label: "Mileage", className: "num" },
          { label: "Seller" },
          { label: "Trust", className: "num" },
          { label: "Waiting", className: "num" },
        ]}
        rows={listings.data?.items ?? []}
        isPending={listings.isPending}
        error={listings.error}
        empty="No listings awaiting review."
        rowKey={(l) => l.id}
        renderRow={(l) => (
          <>
            <td className="nowrap">
              <strong>
                {l.year} {l.make}
              </strong>{" "}
              {l.model}
            </td>
            <td className="grow" dir="auto">
              {l.title}
            </td>
            <td className="num nowrap">{money(l.price)}</td>
            <td className="num nowrap">{l.mileage_km.toLocaleString()} km</td>
            <td className="nowrap">
              {l.seller_name || <span className="faint">unknown</span>}{" "}
              <Tag>{l.seller_kind}</Tag>
            </td>
            <td className="num nowrap">
              <Tag tone={trustTone(l.trust.band)}>
                {Math.round(l.trust.score)} · {l.trust.band}
              </Tag>
            </td>
            <td className="num nowrap">{since(l.submitted_at)}</td>
          </>
        )}
        renderDetail={(l, close) => (
          <Decide onDone={close} onDecide={(approve, reason) => decideListing(l.id, approve, reason)}>
            <Evidence
              rows={[
                // Shown in full. A VIN is the single most checkable fact on a
                // listing and truncating it defeats the check.
                [
                  "VIN",
                  l.vin ? (
                    <span className="mono">{l.vin}</span>
                  ) : (
                    <span className="danger">none supplied</span>
                  ),
                ],
                ["Spec", `${l.regional_spec} · ${l.city ?? "no city"}`],
                ["Seller", <span className="mono">{l.seller_id}</span>],
                [
                  "Trust",
                  <>
                    {Math.round(l.trust.score)} · {l.trust.band}
                  </>,
                ],
                ["Listing", <span className="mono">{l.id}</span>],
              ]}
            />
          </Decide>
        )}
      />

      <QueuePanel
        title="Fraud signals"
        hint="only a person can restrict an account"
        columns={[
          { label: "Kind" },
          { label: "Severity", className: "num" },
          { label: "Subject", className: "grow" },
          { label: "Score now", className: "num" },
          { label: "If confirmed", className: "num" },
          { label: "Raised", className: "num" },
        ]}
        rows={fraud.data?.items ?? []}
        isPending={fraud.isPending}
        error={fraud.error}
        empty="No open signals."
        rowKey={(s) => `${s.subject_type}:${s.subject_id}:${s.kind}`}
        renderRow={(s) => (
          <>
            <td className="nowrap">
              <Tag tone="danger" code>
                {s.kind}
              </Tag>
            </td>
            <td className="num">{s.severity}</td>
            <td className="grow mono">
              {s.subject_type} {s.subject_id}
            </td>
            <td className="num">
              <Tag tone={trustTone(s.band)}>{Math.round(s.score)}</Tag>
            </td>
            {/* The consequence, before the decision — and the *total*, not this
                signal's share. "Does this restrict them" is a question about
                where the score lands, and QA found this figure quoting more than
                twice the number actually applied. */}
            <td className="num">
              <Tag tone={s.score_if_confirmed < 55 ? "danger" : "warn"}>
                {s.score_if_confirmed} (−{s.cost_if_confirmed})
              </Tag>
            </td>
            <td className="num nowrap">{since(s.created_at)}</td>
          </>
        )}
        renderDetail={(s, close) => (
          <Decide
            reasonRequired={false}
            approveLabel="Confirm"
            rejectLabel="Dismiss"
            onDone={close}
            onDecide={(approve) =>
              decideFraud({
                subject_type: s.subject_type,
                subject_id: s.subject_id,
                kind: s.kind,
                confirm: approve,
              })
            }
          >
            <p className={s.score_if_confirmed < 55 ? "note danger" : "note"}>
              Confirming costs {s.cost_if_confirmed} points, leaving this seller
              at {s.score_if_confirmed}.
              {s.score_if_confirmed < 55 && " That restricts the account."}{" "}
              No machine signal can restrict an account on its own — only a
              person confirming one can, which is what this decision is.
            </p>
            <Evidence
              rows={[
                ["Signal", <span className="mono">{s.kind}</span>],
                ["Severity", `${s.severity} of 100`],
                ["Subject", <span className="mono">{s.subject_id}</span>],
                ["Current band", s.band],
              ]}
            />
          </Decide>
        )}
      />
    </>
  );
}
