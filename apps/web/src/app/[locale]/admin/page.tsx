"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use } from "react";

import { getQueues } from "@/lib/admin-api";
import { POLL_MS } from "@/components/admin/shell";
import { waited } from "@/components/admin/table";

/**
 * The screen an ops shift starts on.
 *
 * The design decision here is **which number is large**. Depth is the obvious
 * choice and it is the wrong one: a deep and *young* queue is a busy afternoon,
 * a shallow and *old* one is a queue nobody is working, and only the second is
 * an incident. A dashboard showing depth alone reports them identically.
 *
 * So each tile carries the count, the oldest wait, and the promise that queue
 * makes — the SLA travelling from the server's own table rather than being a
 * number the client picked. A breach turns the whole tile red.
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

  if (error) {
    return (
      <div className="panel">
        <div className="state error">
          <div className="headline">Could not load the queues.</div>
          This account needs a <code>platform_admins</code> row. Staff access is
          checked on the server on every call, never carried in a token claim —
          so granting it takes effect immediately, and so does revoking it.
        </div>
      </div>
    );
  }

  const queues = data?.queues ?? [];
  const breached = queues.filter((q) => q.sla_breached);
  const waiting = queues.reduce((n, q) => n + q.waiting, 0);

  return (
    <>
      {isPending ? (
        <p className="console-lede">Loading…</p>
      ) : breached.length > 0 ? (
        <p className="console-lede danger">
          <strong>
            {breached.length === 1 ? "One queue is" : `${breached.length} queues are`} past
            the promise made to the people waiting on it
          </strong>{" "}
          — {breached.map((q) => LABELS[q.queue] ?? q.queue).join(", ")}.
        </p>
      ) : (
        <p className="console-lede">
          {waiting === 0
            ? "Nothing is waiting."
            : `${waiting} item${waiting === 1 ? "" : "s"} waiting, all within SLA.`}
        </p>
      )}

      <div className="stats">
        {queues.map((q) => (
          <Link
            key={q.queue}
            href={`/${locale}/admin${ROUTES[q.queue] ?? ""}`}
            className={`stat ${q.sla_breached ? "breached" : ""} ${q.waiting === 0 ? "idle" : ""}`}
          >
            <span className="k">{LABELS[q.queue] ?? q.queue}</span>
            <span className="v">{q.waiting}</span>
            <span className="sub">
              {q.waiting === 0
                ? "clear"
                : `oldest ${waited(q.oldest_seconds)} · SLA ${waited(q.sla_seconds)}`}
            </span>
          </Link>
        ))}
      </div>

      <section className="panel">
        <div className="panel-head">
          <h2>What this does not show</h2>
        </div>
        <div className="panel-body">
          <p className="console-lede" style={{ margin: 0 }}>
            Throughput, reviewer productivity and decision-reversal rates. The
            first two because a moderation team measured on items-per-hour
            approves faster, which is the exact failure GCC content standards
            make expensive. The third is worth building and is not built:{" "}
            <em>
              the rate at which appeals succeed is the single most useful measure
              of whether policy is being applied correctly
            </em>
            , and voided strikes are retained specifically so it can be computed.
          </p>
        </div>
      </section>
    </>
  );
}
