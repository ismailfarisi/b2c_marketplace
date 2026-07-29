"use client";

import { useQuery } from "@tanstack/react-query";
import { use } from "react";

import { Detail, Facts, Panel, Ref, TimelinePanel, plural } from "@/components/admin/detail";
import { Tag, money, since } from "@/components/admin/table";
import { getListingDetail, getListingTimeline } from "@/lib/admin-api";

/**
 * The listing page.
 *
 * Opened with "should this stay up", so the case against it comes first: fraud
 * signals, then who is selling, then the car. A reviewer who reads the
 * specification before the seller's history has already formed a view of a
 * perfectly ordinary Land Cruiser by the time they learn the odometer reading
 * went backwards.
 *
 * The number doing the most work here is the seller's other listings. A private
 * seller with eleven cars is a dealer who has not said so — different fees,
 * different consumer protections, different tax treatment — and it is completely
 * invisible from the listing itself.
 */

export default function ListingPage({
  params,
}: {
  params: Promise<{ locale: string; listingId: string }>;
}) {
  const { locale, listingId } = use(params);

  const { data, isLoading, error } = useQuery({
    queryKey: ["admin", "listing", listingId],
    queryFn: () => getListingDetail(listingId),
  });

  if (error) return <p className="danger">This listing could not be loaded.</p>;
  if (isLoading || !data) return <p className="muted">Loading listing…</p>;

  const open = data.signals.filter((s) => s.state === "open");
  const undeclaredDealer = data.seller.kind === "private" && data.seller.other_listings >= 3;

  return (
    <Detail
      title={`${data.vehicle.year} ${data.vehicle.make} ${data.vehicle.model}`}
      subtitle={`${data.reference_no} · ${data.vehicle.mileage_km.toLocaleString()} km · ${
        [data.city, data.emirate].filter(Boolean).join(", ") || "no location"
      }`}
      tags={
        <>
          <Tag tone={statusTone(data.status)}>{data.status}</Tag>
          {open.length > 0 ? <Tag tone="danger">{plural(open.length, "open signal")}</Tag> : null}
          {data.photos === 0 ? <Tag tone="warn">no photos</Tag> : null}
        </>
      }
      aside={
        <TimelinePanel
          queryKey={["admin", "listing", listingId, "timeline"]}
          fetcher={() => getListingTimeline(listingId)}
        />
      }
    >
      {undeclaredDealer ? (
        <p className="alarm">
          This seller is registered as <strong>private</strong> and has{" "}
          {plural(data.seller.other_listings, "other listing")}. That is a dealer who has not said
          so — it changes the fees they owe, the consumer protections the buyer gets, and the tax
          treatment of the sale.
        </p>
      ) : null}

      <Panel title="Signals" note={plural(data.signals.length, "signal")}>
        {data.signals.length === 0 ? (
          <p className="muted">
            Nothing flagged against this listing or its seller. This panel stays when empty — an
            absent panel and a clean one look identical.
          </p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Signal</th>
                <th className="num">Severity</th>
                <th>State</th>
                <th>Detail</th>
                <th>When</th>
              </tr>
            </thead>
            <tbody>
              {data.signals.map((s, i) => (
                <tr key={`${s.kind}-${i}`}>
                  <td className="mono">{s.kind}</td>
                  {/* The raw number, not a bucket. The thresholds that decide
                      whether a signal blocks publication are numeric, and
                      showing "high" here would put the boundary in two places
                      that would then disagree. */}
                  <td className={`num ${s.severity >= 70 ? "danger" : ""}`}>{s.severity}</td>
                  <td>
                    <Tag tone={s.state === "open" ? "danger" : undefined}>{s.state}</Tag>
                  </td>
                  <td className="faint mono">{s.detail ?? "—"}</td>
                  <td className="faint">{since(s.at)} ago</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Who is selling">
        <Facts
          rows={[
            [
              "Seller",
              <Ref locale={locale} kind="users" id={data.seller.id}>
                {data.seller.name ?? data.seller.id}
              </Ref>,
            ],
            ["Phone", data.seller.phone ? <span className="mono">{data.seller.phone}</span> : null],
            [
              "Declared as",
              <>
                <Tag tone={undeclaredDealer ? "danger" : undefined}>{data.seller.kind}</Tag>
                {data.seller.dealer && data.seller.dealer_id ? (
                  <Ref locale={locale} kind="vendors" id={data.seller.dealer_id}>
                    {data.seller.dealer}
                  </Ref>
                ) : null}
              </>,
            ],
            ["Other listings", data.seller.other_listings],
            ["Sold through us", data.seller.sold],
          ]}
        />
      </Panel>

      <Panel title="The car" note={plural(data.photos, "photo")}>
        <Facts
          rows={[
            ["Price", <strong>{money(data.price)}</strong>],
            ["Mileage", `${data.vehicle.mileage_km.toLocaleString()} km`],
            ["VIN", data.vehicle.vin ? <span className="mono">{data.vehicle.vin}</span> : null],
            ["Spec", data.vehicle.spec],
            ["Body", data.vehicle.body],
            ["Fuel", data.vehicle.fuel],
            ["Colour", data.vehicle.colour],
            [
              "Registration",
              data.vehicle.registration_expiry ? (
                new Date(data.vehicle.registration_expiry) < new Date() ? (
                  <Tag tone="danger">expired {data.vehicle.registration_expiry}</Tag>
                ) : (
                  `until ${data.vehicle.registration_expiry}`
                )
              ) : null,
            ],
            ["Warranty", data.vehicle.warranty_until],
          ]}
        />
      </Panel>

      <Panel title="Lifecycle">
        <Facts
          rows={[
            ["Created", new Date(data.lifecycle.created_at).toLocaleString()],
            [
              "Published",
              data.lifecycle.published_at
                ? new Date(data.lifecycle.published_at).toLocaleString()
                : null,
            ],
            [
              "Expires",
              data.lifecycle.expires_at ? (
                new Date(data.lifecycle.expires_at) < new Date() ? (
                  <Tag tone="warn">expired {data.lifecycle.expires_at.slice(0, 10)}</Tag>
                ) : (
                  new Date(data.lifecycle.expires_at).toLocaleDateString()
                )
              ) : null,
            ],
            ["Featured until", data.lifecycle.featured_until],
            [
              "Last bumped",
              data.lifecycle.bumped_at ? `${since(data.lifecycle.bumped_at)} ago` : null,
            ],
            [
              "Sold",
              data.lifecycle.sold_at ? (
                <>
                  {new Date(data.lifecycle.sold_at).toLocaleDateString()}
                  {data.lifecycle.sold_price ? ` for ${money(data.lifecycle.sold_price)}` : ""}
                </>
              ) : null,
            ],
            [
              "Rejected because",
              data.lifecycle.rejection ? (
                <span className="danger">{data.lifecycle.rejection}</span>
              ) : null,
            ],
          ]}
        />
      </Panel>

      <Panel title="Package">
        {!data.entitlement ? (
          <p className="muted">
            No package purchase found for this seller. A listing up without one usually means a
            manual publish or a purchase that has since been archived — worth checking before
            assuming it was paid for.
          </p>
        ) : (
          <>
            <Facts
              rows={[
                ["Package", `${data.entitlement.name} (${data.entitlement.code})`],
                ["Status", <Tag>{data.entitlement.status}</Tag>],
                [
                  "Listings",
                  `${data.entitlement.listings_used} of ${data.entitlement.listing_quota} used`,
                ],
                [
                  "Bumps",
                  `${data.entitlement.bumps_used} of ${data.entitlement.bump_quota} used`,
                ],
                [
                  "Window",
                  data.entitlement.ends_at
                    ? `until ${new Date(data.entitlement.ends_at).toLocaleDateString()}`
                    : "open-ended",
                ],
              ]}
            />
            {/* Stated because it is the surprising part: the purchase belongs to
                the seller, not this listing, so the answer to "why did this
                expire early" is usually the package window rather than anything
                about the car. */}
            <p className="muted">
              A package grants a quota to the seller rather than paying for one listing, so this
              window governs every listing they publish under it.
            </p>
          </>
        )}
      </Panel>
    </Detail>
  );
}

function statusTone(status: string): "danger" | "warn" | "ok" | undefined {
  if (status === "published" || status === "sold") return "ok";
  if (status === "rejected" || status === "removed") return "danger";
  if (status === "pending_review") return "warn";
  return undefined;
}
