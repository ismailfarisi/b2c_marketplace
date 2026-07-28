"use client";

import { getCampaignQueue, reviewCampaign } from "@/lib/admin-api";
import { Decide, QueueState, useQueueData } from "@/components/admin/review";

/**
 * Campaign review.
 *
 * **Not a second content review.** The reel was moderated before it could be
 * published, and re-judging it here would be both wasted effort and a second
 * inconsistent verdict on the same video. What this queue asks is different: is
 * a vendor paying to amplify a reel about something they are licensed to sell,
 * is the targeting sane, is this somebody else's video.
 *
 * The money is prominent because the cost of leaving a campaign here is
 * asymmetric. An unreviewed campaign is an advertiser who has *already paid* and
 * whose money is doing nothing — the worst state available, worse than a
 * rejection, because a rejection at least gets refunded.
 */
function money(m: { amount: number; currency: string }) {
  return `${(m.amount / 100).toLocaleString("en-AE", {
    minimumFractionDigits: 0,
    maximumFractionDigits: 2,
  })} ${m.currency}`;
}

export default function CampaignQueue() {
  const { data, isPending, error } = useQueueData("campaigns", getCampaignQueue);
  const items = data?.items ?? [];

  return (
    <>
      <h1>Campaigns</h1>
      <p className="muted">
        Twenty-four-hour SLA. Everything here is funded and idle — the
        advertiser has paid and nothing is running.
      </p>

      <QueueState
        isPending={isPending}
        error={error}
        count={items.length}
        empty="No campaigns awaiting review."
      />

      <ul className="stack">
        {items.map((c) => (
          <li key={c.id} className="card">
            <div className="row spread">
              <strong dir="auto">{c.advertiser_name || c.id}</strong>
              <span className="pill">{c.reference}</span>
            </div>

            <p dir="auto">{c.caption || <span className="muted">no caption</span>}</p>

            <div className="scroll">
              <table>
                <tbody>
                  <tr>
                    <th>Funded</th>
                    <td>{money(c.funded)}</td>
                  </tr>
                  <tr>
                    <th>Daily cap</th>
                    <td>{money(c.daily_cap)}</td>
                  </tr>
                  <tr>
                    <th>Bid</th>
                    <td>{money(c.bid_cpm)} per thousand impressions</td>
                  </tr>
                  <tr>
                    <th>Starts</th>
                    <td>{c.starts_on}</td>
                  </tr>
                  <tr>
                    <th>Targeting</th>
                    {/* A sentence, composed server-side. Targeting narrows and
                        never widens — a 50 km radius does not extend a
                        vendor's reach past their service area, and the row
                        says so rather than implying otherwise with a map. */}
                    <td>{c.targeting}</td>
                  </tr>
                </tbody>
              </table>
            </div>

            <p className="mono muted">reel {c.reel_id}</p>

            <Decide onDecide={(approve, note) => reviewCampaign(c.id, approve, note)} />
          </li>
        ))}
      </ul>
    </>
  );
}
