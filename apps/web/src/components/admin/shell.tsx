"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { type ReactNode } from "react";

import { getQueues, type QueueDepth } from "@/lib/admin-api";

/**
 * The console frame.
 *
 * The sidebar carries **live queue counts**, and that is the whole reason it
 * earns its 240px. A navigation list that only navigates could be a row of tabs;
 * one that shows where the work is means a moderator finishing a queue looks
 * left rather than clicking through four screens to find the next one. A
 * breached queue turns red there — the one place they are guaranteed to be
 * looking.
 *
 * Grouped rather than flat because the queues answer different questions and are
 * usually staffed by different people. Content moderation is judged in seconds
 * by someone reading Arabic; compliance is judged in minutes by someone
 * comparing a document to a company record. Interleaving them in one list makes
 * every reviewer scan past six things that are not their job.
 */

type Section = { href: string; label: string; queues: string[] };

const GROUPS: Array<{ title: string; items: Section[] }> = [
  {
    title: "Content",
    items: [
      { href: "/reels", label: "Reels", queues: ["reel"] },
      { href: "/comments", label: "Comments", queues: ["comment"] },
      { href: "/messages", label: "Messages", queues: ["message"] },
      { href: "/appeals", label: "Appeals", queues: ["appeal"] },
    ],
  },
  {
    title: "Marketplace",
    items: [
      // Listings and fraud share a screen: a listing review asks "publish this
      // car" and a fraud signal asks "should this seller keep selling", and a
      // reviewer holding one without the other approves a car from someone with
      // two confirmed rollbacks.
      { href: "/listings", label: "Listings", queues: ["listing", "fraud"] },
      { href: "/campaigns", label: "Campaigns", queues: ["boost"] },
    ],
  },
  {
    title: "Money & trust",
    items: [
      { href: "/kyc", label: "KYC", queues: ["kyc", "payout_account"] },
      { href: "/disputes", label: "Disputes", queues: ["dispute"] },
    ],
  },
];

export const TITLES: Record<string, string> = {
  "": "Overview",
  "/reels": "Reel moderation",
  "/comments": "Comment moderation",
  "/messages": "Held chat messages",
  "/appeals": "Appeals",
  "/listings": "Listings & fraud",
  "/campaigns": "Campaign review",
  "/kyc": "Identity & bank accounts",
  "/disputes": "Disputes",
};

/** How often the sidebar counts and every queue refetch. */
export const POLL_MS = 30_000;

function Badge({ depths, queues }: { depths: QueueDepth[]; queues: string[] }) {
  const mine = depths.filter((d) => queues.includes(d.queue));
  const waiting = mine.reduce((n, d) => n + d.waiting, 0);
  const breached = mine.some((d) => d.sla_breached);

  return (
    <span
      className={`console-count ${breached ? "breached" : ""} ${waiting === 0 ? "zero" : ""}`}
      title={breached ? "past its SLA" : undefined}
    >
      {waiting}
    </span>
  );
}

export function ConsoleShell({
  locale,
  children,
}: {
  locale: string;
  children: ReactNode;
}) {
  const pathname = usePathname();
  const base = `/${locale}/admin`;
  const section = pathname.startsWith(base) ? pathname.slice(base.length) : "";

  const { data, isFetching, error } = useQuery({
    queryKey: ["admin", "queues"],
    queryFn: getQueues,
    refetchInterval: POLL_MS,
    placeholderData: (previous) => previous,
  });
  const depths = data?.queues ?? [];

  return (
    <div className="console">
      <nav className="console-nav" aria-label="Sections">
        <div className="console-brand">
          <span>Ops</span>
          {/* Which environment this is, permanently. Staff tooling that looks
              identical in staging and production is how someone refunds a real
              order while testing. */}
          <span className="env">{process.env.NEXT_PUBLIC_ENV ?? "dev"}</span>
        </div>

        <Link href={base} className={`console-link ${section === "" ? "active" : ""}`}>
          <span className="label">Overview</span>
        </Link>

        {GROUPS.map((group) => (
          <div key={group.title}>
            <div className="console-group">{group.title}</div>
            {group.items.map((item) => (
              <Link
                key={item.href}
                href={`${base}${item.href}`}
                className={`console-link ${section === item.href ? "active" : ""}`}
                title={item.label}
              >
                <span className="label">{item.label}</span>
                <Badge depths={depths} queues={item.queues} />
              </Link>
            ))}
          </div>
        ))}
      </nav>

      <div className="console-main">
        <header className="console-appbar">
          <h1>{TITLES[section] ?? "Ops"}</h1>
          <div className="spacer" />
          <div className="meta">
            {error ? (
              <span className="danger">counts unavailable</span>
            ) : (
              <>
                <span className={`console-pulse ${isFetching ? "fetching" : ""}`} />
                <span>every {POLL_MS / 1000}s</span>
              </>
            )}
          </div>
        </header>

        <div className="console-content">{children}</div>
      </div>
    </div>
  );
}
