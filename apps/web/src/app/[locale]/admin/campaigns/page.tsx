"use client";

import { getCampaignQueue, reviewCampaign } from "@/lib/admin-api";
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
 * Campaign review.
 *
 * **Not a second content review.** The reel was moderated before it could be
 * published, and re-judging it here would be both wasted effort and a second
 * inconsistent verdict on the same video. What this queue asks is different: is
 * a vendor paying to amplify a reel about something they are licensed to sell,
 * is the targeting sane, is this somebody else's video.
 *
 * The money is a column because the cost of leaving a campaign here is
 * asymmetric. An unreviewed campaign is an advertiser who has *already paid* and
 * whose money is doing nothing — the worst state available, worse than a
 * rejection, because a rejection at least gets refunded.
 */
export default function CampaignQueue() {
  const { data, isPending, error } = useQueueData("campaigns", getCampaignQueue);
  const rows = data?.items ?? [];

  return (
    <>
      <p className="console-lede">
        Twenty-four-hour SLA. Everything here is funded and idle — the advertiser
        has paid and nothing is running.
      </p>

      <QueuePanel
        title="Awaiting approval"
        hint="reviewing the campaign, not the reel"
        columns={[
          { label: "Advertiser" },
          { label: "Reference" },
          { label: "Creative", className: "grow" },
          { label: "Funded", className: "num" },
          { label: "Daily cap", className: "num" },
          { label: "Bid CPM", className: "num" },
          { label: "Waiting", className: "num" },
        ]}
        rows={rows}
        isPending={isPending}
        error={error}
        empty="No campaigns awaiting review."
        rowKey={(c) => c.id}
        renderRow={(c) => (
          <>
            <td className="nowrap" dir="auto">
              {c.advertiser_name || <span className="faint">unknown</span>}{" "}
              <Tag>{c.advertiser_type}</Tag>
            </td>
            <td className="mono nowrap">{c.reference}</td>
            <td className="grow" dir="auto">
              {c.caption || <span className="faint">no caption</span>}
            </td>
            <td className="num nowrap">{money(c.funded)}</td>
            <td className="num nowrap">{money(c.daily_cap)}</td>
            <td className="num nowrap">{money(c.bid_cpm)}</td>
            <td className="num nowrap">{since(c.created_at)}</td>
          </>
        )}
        renderDetail={(c, close) => (
          <Decide onDone={close} onDecide={(approve, note) => reviewCampaign(c.id, approve, note)}>
            <Evidence
              rows={[
                ["Reel", <span className="mono">{c.reel_id}</span>],
                ["Caption", <span dir="auto">{c.caption}</span>],
                ["Starts", c.starts_on],
                // A sentence, composed server-side. Targeting narrows and never
                // widens — a 50 km radius does not extend a vendor's reach past
                // their service area, and the row says so rather than implying
                // otherwise with a map.
                ["Targeting", c.targeting],
              ]}
            />
            <p className="note">
              The reel was moderated before it could be published. What this
              decision covers is the campaign: is this advertiser entitled to
              amplify this content, and is the targeting sane.
            </p>
          </Decide>
        )}
      />
    </>
  );
}
