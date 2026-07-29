"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useState, type ReactNode } from "react";

import type { TimelineEntry } from "@/lib/admin-api";

import { since } from "./table";

/**
 * The parts a detail page is made of.
 *
 * A queue screen answers "what needs doing" and a detail page answers "what is
 * going on with this thing" — and the second question is the one that arrives
 * by phone, with a customer waiting. So the shapes here optimise for *reading
 * quickly under pressure* rather than for density: facts in one column that can
 * be scanned, a timeline that is honest about what is not known, and actions
 * that make you say why.
 *
 * Three components carry almost every page in the console. Writing them once is
 * what stops the order page and the vendor page from disagreeing about what a
 * timestamp looks like, which sounds cosmetic until someone reads one as local
 * time and the other as UTC on the same call.
 */

// ── layout ──────────────────────────────────────────────────────────────────

/**
 * The page frame: a title row, then a wide column and a narrow one.
 *
 * The narrow column is always the timeline. Fixing its position across every
 * detail page means "when did that happen" is answered by looking in the same
 * place regardless of what is being looked at.
 */
export function Detail({
  title,
  subtitle,
  tags,
  actions,
  aside,
  children,
}: {
  title: ReactNode;
  subtitle?: ReactNode;
  tags?: ReactNode;
  actions?: ReactNode;
  aside: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="detail">
      <div className="detail-head">
        <div>
          <h2>
            {title}
            {tags}
          </h2>
          {subtitle ? <p className="console-lede">{subtitle}</p> : null}
        </div>
        <div className="spacer" />
        {actions}
      </div>

      <div className="detail-body">
        <div className="detail-main">{children}</div>
        <aside className="detail-aside">{aside}</aside>
      </div>
    </div>
  );
}

/** A titled box. Everything on a detail page lives in one. */
export function Panel({
  title,
  note,
  children,
}: {
  title: string;
  note?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="panel">
      <header className="panel-head">
        <h3>{title}</h3>
        <div className="spacer" />
        {note ? <span className="muted">{note}</span> : null}
      </header>
      <div className="panel-body">{children}</div>
    </section>
  );
}

// ── facts ───────────────────────────────────────────────────────────────────

export type Fact = [label: string, value: ReactNode];

/**
 * A definition list, with one rule: a fact that is absent renders as absent.
 *
 * The temptation is to drop empty rows to keep the page tidy. That loses the
 * difference between "this customer has no email" and "this page does not show
 * emails", which is exactly the difference an agent needs when they are about to
 * tell someone their receipt was sent. `—` is a fact; a missing row is not.
 */
export function Facts({ rows }: { rows: Fact[] }) {
  return (
    <dl className="facts">
      {rows.map(([label, value]) => (
        <div key={label}>
          <dt>{label}</dt>
          <dd>{value === null || value === undefined || value === "" ? <span className="faint">—</span> : value}</dd>
        </div>
      ))}
    </dl>
  );
}

// ── timeline ────────────────────────────────────────────────────────────────

const SOURCE_LABEL: Record<string, string> = {
  fulfillment: "delivery",
  payment: "payment",
  ledger: "ledger",
  notification: "message",
  staff: "staff",
};

/**
 * The merged history, from five tables that until recently nothing read back.
 *
 * Source is shown on every row, not just as a colour, because the same word
 * means different things in different places: a `cancelled` from `fulfillment`
 * is a stop being dropped and a `cancelled` from `staff` is a person deciding
 * it. Losing that distinction turns a timeline into a rumour.
 */
export function Timeline({
  entries,
  loading,
  error,
}: {
  entries: TimelineEntry[];
  loading?: boolean;
  error?: unknown;
}) {
  if (error) return <p className="danger">This history could not be loaded.</p>;
  if (loading) return <p className="muted">Loading history…</p>;
  if (entries.length === 0)
    return (
      <p className="muted">
        Nothing recorded yet. That is a real answer, not a missing one — if
        something happened, it would be here.
      </p>
    );

  return (
    <ol className="timeline">
      {entries.map((e, i) => (
        <li key={`${e.at}-${e.source}-${e.kind}-${i}`} className={`tl-${e.source}`}>
          <div className="tl-when" title={new Date(e.at).toLocaleString()}>
            {since(e.at)} ago
          </div>
          <div className="tl-what">
            <span className="tl-kind">{e.kind}</span>
            <span className="tl-source">{SOURCE_LABEL[e.source] ?? e.source}</span>
            {e.detail ? <div className="tl-detail">{e.detail}</div> : null}
            {/* An empty actor is the system, and saying so is the point: "who
                cancelled this" and "it expired" look identical otherwise. */}
            <div className="tl-actor">{e.actor ? e.actor : <span className="faint">system</span>}</div>
          </div>
        </li>
      ))}
    </ol>
  );
}

/** The aside every detail page puts its timeline in. */
export function TimelinePanel({
  queryKey,
  fetcher,
}: {
  queryKey: string[];
  fetcher: () => Promise<{ items: TimelineEntry[] }>;
}) {
  const { data, isLoading, error } = useQuery({ queryKey, queryFn: fetcher });
  return (
    <Panel title="History" note={data ? plural(data.items.length, "event") : undefined}>
      <Timeline entries={data?.items ?? []} loading={isLoading} error={error} />
    </Panel>
  );
}

/** "1 event", "13 events". Small, and the alternative is on screen forever. */
export function plural(n: number, word: string, plural?: string): string {
  return `${n} ${n === 1 ? word : (plural ?? `${word}s`)}`;
}

// ── actions ─────────────────────────────────────────────────────────────────

/**
 * A button that will not fire until a reason has been typed.
 *
 * The reason is required by the database, not just by this form — but making
 * the *UI* refuse is what gets a useful one. A field that can be skipped
 * collects "test" and "as discussed"; a field that blocks the button collects
 * the sentence someone will need in six months when they are asked why the
 * platform refunded 300 dirhams.
 *
 * Two steps on purpose. The first click reveals the form rather than acting, so
 * that a mis-click on a page full of a customer's data cannot move money.
 */
export function Action({
  label,
  verb,
  danger,
  hint,
  extra,
  onConfirm,
  disabled,
  disabledReason,
}: {
  label: string;
  /** What the confirm button says. "Refund", not "Confirm" — the last thing
   *  read before acting should name the act. */
  verb: string;
  danger?: boolean;
  hint?: ReactNode;
  /** Optional inputs above the reason, e.g. a partial refund amount. */
  extra?: (busy: boolean) => ReactNode;
  onConfirm: (reason: string) => Promise<unknown>;
  disabled?: boolean;
  disabledReason?: string;
}) {
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);

  if (done) return <span className="action-done">{done}</span>;

  if (!open) {
    return (
      <button
        type="button"
        className={`btn ${danger ? "danger" : ""}`}
        disabled={disabled}
        title={disabled ? disabledReason : undefined}
        onClick={() => setOpen(true)}
      >
        {label}
      </button>
    );
  }

  return (
    <form
      className={`action ${danger ? "danger" : ""}`}
      onSubmit={async (event) => {
        event.preventDefault();
        setBusy(true);
        setError(null);
        try {
          await onConfirm(reason.trim());
          setDone(`${label} done`);
        } catch (err) {
          // The server's sentence, not a generic failure. Conflicts here say
          // things like "void the authorization instead" — which is the whole
          // answer, and hiding it behind "something went wrong" wastes it.
          setError(messageOf(err));
        } finally {
          setBusy(false);
        }
      }}
    >
      <div className="action-title">{label}</div>
      {hint ? <p className="muted">{hint}</p> : null}
      {extra ? extra(busy) : null}
      <label className="field">
        <span>Reason</span>
        <textarea
          value={reason}
          onChange={(event) => setReason(event.target.value)}
          rows={2}
          placeholder="Why is this being done? Recorded against your name."
          disabled={busy}
        />
      </label>
      {error ? <p className="danger">{error}</p> : null}
      <div className="action-buttons">
        <button
          type="submit"
          className={`btn ${danger ? "danger" : "primary"}`}
          disabled={busy || reason.trim().length < 4}
        >
          {busy ? "Working…" : verb}
        </button>
        {/* "Never mind", not "Cancel". On an order page the destructive action
            is itself called Cancel, and two adjacent buttons reading "Cancel
            this order" and "Cancel" is a coin flip at the exact moment you want
            certainty. */}
        <button type="button" className="btn" disabled={busy} onClick={() => setOpen(false)}>
          Never mind
        </button>
      </div>
    </form>
  );
}

/** Pulls the sentence out of an RFC 7807 problem, falling back sensibly. */
export function messageOf(err: unknown): string {
  const body = (err as { body?: { detail?: string; errors?: Array<{ message?: string }>; title?: string } })
    ?.body;
  if (body?.errors?.length) return body.errors.map((e) => e.message).filter(Boolean).join("; ");
  if (body?.detail) return body.detail;
  if (body?.title) return body.title;
  return err instanceof Error ? err.message : "Request failed.";
}

// ── cross-links ─────────────────────────────────────────────────────────────

/**
 * A link to another record in the console.
 *
 * Every id on a detail page is a link, because the question after "what
 * happened to this order" is nearly always "what else has this customer done" —
 * and an id that has to be copied into a search box is a dead end with extra
 * steps.
 */
export function Ref({
  locale,
  kind,
  id,
  children,
}: {
  locale: string;
  kind: "orders" | "users" | "vendors";
  id: string;
  children: ReactNode;
}) {
  return (
    <Link className="ref" href={`/${locale}/admin/${kind}/${id}`}>
      {children}
    </Link>
  );
}
