"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use } from "react";

import { getQueues } from "@/lib/admin-api";
import { POLL_MS, waited } from "@/components/admin/review";

/**
 * The screen an ops shift starts on.
 *
 * The design decision here is which number is large. Depth is the obvious
 * choice and it is the wrong one: **a deep and young queue is a busy afternoon,
 * a shallow and old one is a queue nobody is working, and only the second is an
 * incident.** A dashboard that shows depth alone reports them identically.
 *
 * So the oldest wait is the prominent figure, the SLA is rendered next to it
 * from the server's own table, and a breach is a colour rather than a footnote.
 *
 * Empty queues are shown too. A tile that disappears when its queue empties is
 * one whose sudden reappearance nobody notices, and "nothing is waiting" is a
 * fact worth seeing rather than an absence.
 */

const ROUTES: Record<string, string> = {
  reel: "/reels",
  comment: "/comments",
  message: "/messages",
  listing: "/listings",
  fraud: "/listings",
  appeal: "/appeals",
  kyc: "/kyc",
  payout_account: "/kyc",
  dispute: "/disputes",
  boost: "/campaigns",
};

const LABELS: Record<string, string> = {
  reel: "Reels",
  comment: "Comments",
  message: "Chat messages",
  listing: "Listings",
  fraud: "Fraud signals",
  appeal: "Appeals",
  kyc: "Identity checks",
  payout_account: "Bank accounts",
  dispute: "Disputes",
  boost: "Campaigns",
};

export default function AdminOverview({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale } = use(params);

  const { data, isPending, error } = useQuery({
    queryKey: ["admin", "queues"],
    queryFn: getQueues,
    refetchInterval: POLL_MS,
    placeholderData: (previous) => previous,
  });

  if (isPending) return <p className="muted">Loading…</p>;

  if (error) {
    return (
      <>
        <h1>Ops</h1>
        <p className="bad">
          Could not load the queues. This account needs a `platform_admins` row —
          staff access is checked on the server on every call, never carried in a
          token claim, so granting it takes effect immediately and so does
          revoking it.
        </p>
      </>
    );
  }

  const queues = data?.queues ?? [];
  const breached = queues.filter((q) => q.sla_breached);
  const totalWaiting = queues.reduce((n, q) => n + q.waiting, 0);

  return (
    <>
      <div className="row spread">
        <h1>Ops</h1>
        <span className="muted">refreshes every {POLL_MS / 1000}s</span>
      </div>

      {breached.length > 0 ? (
        <p className="bad">
          {breached.length === 1 ? "One queue is" : `${breached.length} queues are`} past
          the promise made to the people waiting on it:{" "}
          {breached.map((q) => LABELS[q.queue] ?? q.queue).join(", ")}.
        </p>
      ) : (
        <p className="muted">
          {totalWaiting === 0
            ? "Nothing is waiting."
            : `${totalWaiting} item${totalWaiting === 1 ? "" : "s"} waiting, all within SLA.`}
        </p>
      )}

      <div className="metrics">
        {queues.map((q) => {
          const href = `/${locale}/admin${ROUTES[q.queue] ?? ""}`;
          return (
            <Link
              key={q.queue}
              href={href}
              className={`metric ${q.sla_breached ? "bad" : q.waiting > 0 ? "warn" : ""}`}
            >
              <span className="metric-label">{LABELS[q.queue] ?? q.queue}</span>
              <span className="metric-value">{q.waiting}</span>
              <span className="metric-label">
                {q.waiting === 0 ? (
                  "clear"
                ) : (
                  <>
                    oldest {waited(q.oldest_seconds)}
                    {" · "}
                    <span className={q.sla_breached ? "bad" : "muted"}>
                      SLA {waited(q.sla_seconds)}
                    </span>
                  </>
                )}
              </span>
            </Link>
          );
        })}
      </div>

      <section className="card">
        <h2>What this does not show</h2>
        <p className="muted">
          Throughput, reviewer productivity and decision-reversal rates. The
          first two because a moderation team measured on items-per-hour
          approves faster, which is the exact failure GCC content standards make
          expensive. The third is worth building and is not built:{" "}
          <em>the rate at which appeals succeed is the single most useful
          measure of whether policy is being applied correctly</em>, and voided
          strikes are retained specifically so it can be computed.
        </p>
      </section>
    </>
  );
}
