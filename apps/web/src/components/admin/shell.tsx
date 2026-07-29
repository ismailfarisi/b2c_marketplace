"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";

import { adminSearch, getQueues, type QueueDepth, type SearchHit } from "@/lib/admin-api";

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
      { href: "/supply", label: "Supply", queues: [] },
    ],
  },
  {
    title: "Money & trust",
    items: [
      { href: "/kyc", label: "KYC", queues: ["kyc", "payout_account"] },
      { href: "/disputes", label: "Disputes", queues: ["dispute"] },
      // These four have no queue behind them, and the empty badge is honest:
      // there is no backlog to clear on a ledger. They are here rather than in
      // their own group because "money & trust" is one person's job, and the
      // person checking KYC in the morning is the person who checks the trial
      // balance.
      { href: "/ledger", label: "Ledger", queues: [] },
      { href: "/payouts", label: "Payouts", queues: [] },
      { href: "/cash", label: "Cash", queues: [] },
      { href: "/refunds", label: "Refunds", queues: [] },
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
  "/supply": "Content supply",
  "/kyc": "Identity & bank accounts",
  "/disputes": "Disputes",
  "/ledger": "Ledger & trial balance",
  "/payouts": "Payout runs",
  "/cash": "Cash reconciliation",
  "/refunds": "Refunds & invoices",
};

/** How often the sidebar counts and every queue refetch. */
export const POLL_MS = 30_000;

function Badge({ depths, queues }: { depths: QueueDepth[]; queues: string[] }) {
  // No queue means no backlog to show. A permanent grey zero next to "Ledger"
  // trains the eye to ignore the badge column, which is the one thing it must
  // not do.
  if (queues.length === 0) return null;

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

/**
 * One box for every identifier the platform has.
 *
 * In the app bar rather than on a page, because it is not a destination — the
 * person using it has a customer on the phone reading out something, and does
 * not know whether AE-G3CRYK is an order number, a reference, or a code from an
 * email. Making them pick a category first asks them a question their caller
 * cannot answer.
 *
 * Eight identifier shapes resolve server-side: order numbers, listing and
 * dispute and campaign and statement references, invoice numbers, VINs, phone
 * suffixes, vendor names, the last four of an IBAN, and raw uuids. The client
 * types three characters and waits.
 */
function Search({ locale }: { locale: string }) {
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const term = useDebounced(q.trim(), 250);

  const { data, isFetching } = useQuery({
    queryKey: ["admin", "search", term],
    queryFn: () => adminSearch(term),
    // Three characters is where the server starts answering; asking earlier
    // just returns nothing slower.
    enabled: term.length >= 3,
    staleTime: 30_000,
  });

  const hits = term.length >= 3 ? (data?.items ?? []) : [];

  return (
    <div className="console-search" onBlur={(e) => {
      if (!e.currentTarget.contains(e.relatedTarget)) setOpen(false);
    }}>
      <input
        type="search"
        value={q}
        placeholder="Order no, phone, VIN, reference, IBAN last 4…"
        onChange={(event) => {
          setQ(event.target.value);
          setOpen(true);
        }}
        onFocus={() => setOpen(true)}
        aria-label="Find anything"
      />
      {open && term.length >= 3 ? (
        <div className="console-results">
          {hits.length === 0 ? (
            <div className="console-result empty">
              {isFetching ? "Searching…" : "Nothing matches that."}
            </div>
          ) : (
            hits.map((hit) => (
              <Link
                key={`${hit.kind}-${hit.id}`}
                href={hitHref(locale, hit)}
                className="console-result"
                onClick={() => setOpen(false)}
              >
                <span className="kind">{hit.kind}</span>
                <span className="label">{hit.label}</span>
                <span className="sub">{hit.sub}</span>
              </Link>
            ))
          )}
        </div>
      ) : null}
    </div>
  );
}

/**
 * Where a hit goes.
 *
 * Kinds without a detail page yet land on their queue rather than a 404. A
 * search that finds the thing and then refuses to show it is worse than one
 * that never found it, because the agent now knows it exists.
 */
function hitHref(locale: string, hit: SearchHit): string {
  const base = `/${locale}/admin`;
  switch (hit.kind) {
    case "order":
      return `${base}/orders/${hit.id}`;
    case "user":
      return `${base}/users/${hit.id}`;
    case "vendor":
      return `${base}/vendors/${hit.id}`;
    case "listing":
      return `${base}/listings`;
    case "dispute":
      return `${base}/disputes`;
    case "campaign":
      return `${base}/campaigns`;
    case "reel":
      return `${base}/reels/${hit.id}`;
    default:
      return `${base}`;
  }
}

function useDebounced<T>(value: T, ms: number): T {
  const [held, setHeld] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setHeld(value), ms);
    return () => clearTimeout(timer);
  }, [value, ms]);
  return held;
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
          <Search locale={locale} />
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
