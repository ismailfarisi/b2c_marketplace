"use client";

import { useQuery } from "@tanstack/react-query";
import { use, useState } from "react";

import { Panel, Ref, plural } from "@/components/admin/detail";
import { Tag, money } from "@/components/admin/table";
import {
  getJournals,
  getLedgerEventTypes,
  getTrialBalance,
  type Journal,
} from "@/lib/admin-api";

/**
 * The ledger, in two halves: what the books say, and what moved.
 *
 * Trial balance on top because it is the question with a yes/no answer, and it
 * is the one that should be looked at every morning by someone who then looks at
 * nothing else. Debits equal credits or they do not. If they do not, no number
 * below it is worth reading and the page says so rather than making the reader
 * infer it from two totals.
 *
 * The explorer underneath is the "where did this money go" tool. Every journal
 * shows its own lines and its own balance, because an imbalance is a property of
 * a *posting*, and a page that only totals at the bottom makes the reader binary
 * search for the row that broke it.
 */

export default function LedgerPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = use(params);
  const [eventType, setEventType] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");

  const balance = useQuery({
    queryKey: ["admin", "trial-balance", from, to],
    queryFn: () => getTrialBalance({ from, to }),
  });

  const types = useQuery({
    queryKey: ["admin", "ledger", "event-types"],
    queryFn: getLedgerEventTypes,
    staleTime: 5 * 60_000,
  });

  const journals = useQuery({
    queryKey: ["admin", "ledger", eventType, from, to],
    queryFn: () => getJournals({ event_type: eventType, from, to, limit: 50 }),
  });

  if (balance.error) {
    return (
      <p className="danger">
        The ledger could not be loaded. If this reads &ldquo;forbidden&rdquo;, the account is missing
        <span className="mono"> money.read</span> — which is a permission, not a bug.
      </p>
    );
  }

  const tb = balance.data;

  return (
    <div className="detail">
      <div className="detail-head">
        <div>
          <h2>
            Ledger
            {tb ? (
              <Tag tone={tb.balanced ? "ok" : "danger"}>
                {tb.balanced ? "balanced" : "OUT OF BALANCE"}
              </Tag>
            ) : null}
          </h2>
          <p className="console-lede">
            Every posting the platform has made, and whether the books agree with themselves.
          </p>
        </div>
        <div className="spacer" />
        <div className="filters">
          <label className="field">
            <span>From</span>
            <input type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
          </label>
          <label className="field">
            <span>To</span>
            <input type="date" value={to} onChange={(e) => setTo(e.target.value)} />
          </label>
        </div>
      </div>

      {tb && !tb.balanced ? (
        <p className="alarm">
          Debits and credits disagree by{" "}
          <strong>
            {money({
              amount: Math.abs(tb.total_debit.amount - tb.total_credit.amount),
              currency: tb.currency,
            })}
          </strong>
          . Every journal is balanced at write time, so an imbalance here means entries were written
          outside the journal path. Nothing below this line should be trusted until it is explained.
        </p>
      ) : null}

      <Panel
        title="Trial balance"
        note={tb ? `${plural(tb.accounts.length, "account")} · as of ${new Date(tb.as_of).toLocaleString()}` : undefined}
      >
        {balance.isLoading || !tb ? (
          <p className="muted">Loading…</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Account</th>
                <th className="num">Debit</th>
                <th className="num">Credit</th>
                <th className="num">Net</th>
                <th className="num">Entries</th>
              </tr>
            </thead>
            <tbody>
              {tb.accounts.map((a) => (
                <tr key={`${a.type}-${a.owner_id ?? "platform"}`}>
                  <td>
                    <span className="mono">{a.type}</span>
                    {a.owner_id ? (
                      <span className="faint"> · {a.owner_type}</span>
                    ) : (
                      <span className="faint"> · platform</span>
                    )}
                  </td>
                  <td className="num">{money(a.debit)}</td>
                  <td className="num">{money(a.credit)}</td>
                  {/* Not coloured. A negative net is a debit balance, which is
                      correct and expected on a float or receivable account —
                      and in this stylesheet red means "past the promise we
                      made". Spending it on "this number has a minus sign"
                      is how a real breach stops registering. */}
                  <td className="num">{money(a.net)}</td>
                  {/* An account with no movement is kept and shown faint. "This
                      account should have activity and has none" is a finding,
                      and dropping the row hides it. */}
                  <td className={`num ${a.entries === 0 ? "faint" : ""}`}>{a.entries}</td>
                </tr>
              ))}
              <tr className="total">
                <td>Total</td>
                <td className="num">{money(tb.total_debit)}</td>
                <td className="num">{money(tb.total_credit)}</td>
                <td className="num" colSpan={2}>
                  {tb.balanced ? "✓" : "✗"}
                </td>
              </tr>
            </tbody>
          </table>
        )}
      </Panel>

      <Panel
        title="Postings"
        note={
          <select value={eventType} onChange={(e) => setEventType(e.target.value)}>
            <option value="">every event type</option>
            {(types.data?.items ?? []).map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
        }
      >
        {journals.isLoading ? (
          <p className="muted">Loading…</p>
        ) : (journals.data?.items ?? []).length === 0 ? (
          <p className="muted">Nothing posted in this range.</p>
        ) : (
          <ol className="journals">
            {journals.data!.items.map((j) => (
              <JournalRow key={j.id} journal={j} locale={locale} />
            ))}
          </ol>
        )}
      </Panel>
    </div>
  );
}

function JournalRow({ journal, locale }: { journal: Journal; locale: string }) {
  return (
    <li className={journal.balanced ? "" : "unbalanced"}>
      <div className="journal-head">
        <span className="mono">{journal.event_type}</span>
        {journal.reference_type === "order" && journal.reference_id ? (
          <Ref locale={locale} kind="orders" id={journal.reference_id}>
            the order
          </Ref>
        ) : journal.reference_type ? (
          <span className="faint">
            {journal.reference_type} {journal.reference_id?.slice(0, 8)}
          </span>
        ) : null}
        {!journal.balanced ? <Tag tone="danger">does not balance</Tag> : null}
        <div className="spacer" />
        <span className="faint">{new Date(journal.occurred_at).toLocaleString()}</span>
      </div>
      {journal.description ? <div className="faint">{journal.description}</div> : null}
      <table className="money-table">
        {/* Dr and Cr headers on every journal, short and repeated. Two unlabelled
            columns of money is a guess, and the reader who guesses wrong reads
            a refund as a charge. */}
        <thead>
          <tr>
            <th>Account</th>
            <th className="num">Dr</th>
            <th className="num">Cr</th>
          </tr>
        </thead>
        <tbody>
          {journal.lines.map((l, i) => (
            <tr key={`${l.account_id}-${i}`}>
              <td className="mono">{l.account_type}</td>
              <td className="num">{l.direction === "debit" ? money(l.amount) : ""}</td>
              <td className="num">{l.direction === "credit" ? money(l.amount) : ""}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </li>
  );
}
