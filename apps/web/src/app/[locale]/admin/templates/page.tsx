"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { Action, Panel, messageOf, plural } from "@/components/admin/detail";
import { Tag, money, since } from "@/components/admin/table";
import {
  getRateCards,
  getTaxRates,
  getTemplates,
  previewSegments,
  reviseTemplate,
  supersedeRateCard,
  supersedeTaxRate,
  type NotificationTemplate,
} from "@/lib/admin-api";

/**
 * The versioned editors — rates, prices and messages.
 *
 * None of these is an edit. Every change here closes the current row and opens a
 * new one, because all three are quoted in documents that have already gone to
 * customers: an invoice references the VAT rate that was in force when it was
 * raised, a campaign was priced against the floor that was live when it booked,
 * and a support agent reading "we told them X" needs the text that was actually
 * sent rather than a reconstruction from a template edited since.
 *
 * The consequence an operator has to feel is that **nothing takes effect today**.
 * An effective date is required and must be in the future, because backdating
 * restates documents that are already out — and that is a credit note, not a
 * configuration change.
 */

export default function TemplatesPage() {
  const queryClient = useQueryClient();
  const taxRates = useQuery({ queryKey: ["admin", "tax-rates"], queryFn: getTaxRates });
  const rateCards = useQuery({ queryKey: ["admin", "rate-cards"], queryFn: getRateCards });
  const templates = useQuery({ queryKey: ["admin", "templates"], queryFn: () => getTemplates() });

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["admin", "tax-rates"] });
    void queryClient.invalidateQueries({ queryKey: ["admin", "rate-cards"] });
    void queryClient.invalidateQueries({ queryKey: ["admin", "templates"] });
  };

  if (taxRates.error) {
    return (
      <p className="danger">
        Rates could not be loaded. If this reads &ldquo;forbidden&rdquo;, the account is missing
        <span className="mono"> health.read</span>.
      </p>
    );
  }

  const rates = taxRates.data?.items ?? [];
  const cards = rateCards.data?.items ?? [];

  return (
    <div className="detail">
      <div className="detail-head">
        <div>
          <h2>Rates &amp; messages</h2>
          <p className="console-lede">
            Nothing here is edited in place. A change closes the current row and opens a new one
            from a future date, so documents already issued keep referencing what was true when they
            were issued.
          </p>
        </div>
      </div>

      <Panel title="Tax rates" note={plural(rates.length, "rate")}>
        <table className="money-table">
          <thead>
            <tr>
              <th>Where</th>
              <th>Category</th>
              <th className="num">Rate</th>
              <th>In force</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {rates.map((t) => (
              <tr key={t.id} className={t.current ? "" : "historic"}>
                <td>
                  {t.country}
                  {t.region ? <span className="faint"> · {t.region}</span> : null}
                </td>
                <td>{t.category}</td>
                <td className="num">{(t.rate_bp / 100).toFixed(2)}%</td>
                <td className="faint">
                  {t.from}
                  {/* An open-ended row is the one in force. A closed one is
                      history and is kept visible rather than filtered away —
                      "what was the rate in March" is a question that gets
                      asked. */}
                  {t.to ? ` – ${t.to}` : " onwards"}
                  {t.current ? <Tag tone="ok">current</Tag> : null}
                </td>
                <td>
                  {t.current ? (
                    <SupersedeRate rate={t} onDone={invalidate} />
                  ) : (
                    <span className="faint">closed</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Panel>

      <Panel title="Boost floor prices" note={plural(cards.length, "card")}>
        {cards.length === 0 ? (
          <p className="muted">No rate cards. Campaigns have no reserve price without one.</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Placement</th>
                <th>Where</th>
                <th className="num">Floor CPM</th>
                <th>In force</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {cards.map((c) => (
                <tr key={c.id} className={c.current ? "" : "historic"}>
                  <td>{c.placement}</td>
                  <td>
                    {c.country}
                    {c.region ? <span className="faint"> · {c.region}</span> : null}
                  </td>
                  <td className="num">{money(c.floor)}</td>
                  <td className="faint">
                    {c.from}
                    {c.to ? ` – ${c.to}` : " onwards"}
                    {c.current ? <Tag tone="ok">current</Tag> : null}
                  </td>
                  <td>
                    {c.current ? (
                      <SupersedeCard card={c} onDone={invalidate} />
                    ) : (
                      <span className="faint">closed</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel
        title="Message templates"
        note={plural(templates.data?.items.length ?? 0, "active template")}
      >
        {templates.isLoading ? (
          <p className="muted">Loading…</p>
        ) : (
          <div className="templates">
            {(templates.data?.items ?? []).map((t) => (
              <TemplateEditor key={t.id} template={t} onDone={invalidate} />
            ))}
          </div>
        )}
      </Panel>
    </div>
  );
}

function SupersedeRate({
  rate,
  onDone,
}: {
  rate: { id: string; rate_bp: number };
  onDone: () => void;
}) {
  const [pct, setPct] = useState((rate.rate_bp / 100).toString());
  const [from, setFrom] = useState(tomorrow());

  return (
    <Action
      label="Supersede"
      verb="Close this rate and open the new one"
      hint="The current rate is closed the day before the new one starts. Both remain readable; documents issued under the old one keep referencing it."
      extra={(busy) => (
        <>
          <label className="field">
            <span>New rate (%)</span>
            <input
              type="number"
              step="0.01"
              min="0"
              max="100"
              value={pct}
              onChange={(e) => setPct(e.target.value)}
              disabled={busy}
            />
          </label>
          <label className="field">
            <span>First day it applies</span>
            <input
              type="date"
              value={from}
              min={tomorrow()}
              onChange={(e) => setFrom(e.target.value)}
              disabled={busy}
            />
          </label>
        </>
      )}
      onConfirm={async (reason) => {
        await supersedeTaxRate(rate.id, {
          rate_bp: Math.round(Number(pct) * 100),
          effective_from: from,
          reason,
        });
        onDone();
      }}
    />
  );
}

function SupersedeCard({
  card,
  onDone,
}: {
  card: { id: string; floor: { amount: number; currency: string } };
  onDone: () => void;
}) {
  const [major, setMajor] = useState((card.floor.amount / 100).toString());
  const [from, setFrom] = useState(tomorrow());

  return (
    <Action
      label="Supersede"
      verb="Close this card and open the new one"
      hint="Campaigns already booked keep the floor they were priced against."
      extra={(busy) => (
        <>
          <label className="field">
            <span>New floor CPM ({card.floor.currency})</span>
            <input
              type="number"
              step="0.01"
              min="0"
              value={major}
              onChange={(e) => setMajor(e.target.value)}
              disabled={busy}
            />
          </label>
          <label className="field">
            <span>First day it applies</span>
            <input
              type="date"
              value={from}
              min={tomorrow()}
              onChange={(e) => setFrom(e.target.value)}
              disabled={busy}
            />
          </label>
        </>
      )}
      onConfirm={async (reason) => {
        await supersedeRateCard(card.id, {
          floor_cpm_minor: Math.round(Number(major) * 100),
          effective_from: from,
          reason,
        });
        onDone();
      }}
    />
  );
}

/**
 * The template editor, with the cost of a send shown while you type.
 *
 * The segment count is the point. A character count tells an operator nothing,
 * because the number that matters is not how long the message is but which
 * encoding it forces: anything outside GSM-7 drops the budget from 160
 * characters to 70. Arabic is entirely outside it, so an Arabic message can be
 * shorter than an English one and cost twice as much — and one en-dash pasted
 * from a document does the same thing to an English message.
 *
 * Counted server-side so the number shown and the number charged come from the
 * same code, and debounced because it is a request per keystroke otherwise.
 */
function TemplateEditor({
  template,
  onDone,
}: {
  template: NotificationTemplate;
  onDone: () => void;
}) {
  const [draft, setDraft] = useState(template.body);
  const [subject, setSubject] = useState(template.subject ?? "");
  const [preview, setPreview] = useState<{ segments: number; characters: number } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const dirty = draft !== template.body || subject !== (template.subject ?? "");

  useEffect(() => {
    if (!dirty) {
      setPreview(null);
      return;
    }
    const timer = setTimeout(() => {
      previewSegments(draft)
        .then((p) => setPreview(p))
        .catch(() => setPreview(null));
    }, 300);
    return () => clearTimeout(timer);
  }, [draft, dirty]);

  const shown = preview ?? { segments: template.segments, characters: template.body.length };
  const costlier = preview !== null && preview.segments > template.segments;

  return (
    <div className="template">
      <div className="template-head">
        <span className="mono">{template.key}</span>
        <Tag>{template.channel}</Tag>
        <Tag>{template.locale}</Tag>
        <span className="faint">v{template.version}</span>
        <div className="spacer" />
        <span className={costlier ? "danger" : "faint"}>
          {shown.characters} chars · {plural(shown.segments, "segment")}
          {costlier ? ` (was ${template.segments})` : ""}
        </span>
      </div>

      {template.channel === "email" ? (
        <label className="field">
          <span>Subject</span>
          <input value={subject} onChange={(e) => setSubject(e.target.value)} />
        </label>
      ) : null}

      <textarea
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        rows={3}
        dir="auto"
        spellCheck={false}
      />

      <div className="template-foot">
        <span className="faint mono">
          {template.placeholders.map((p) => `{{${p}}}`).join(" ") || "no placeholders"}
        </span>
        <div className="spacer" />
        {dirty ? (
          <Action
            label="Save as new version"
            verb={`Write version ${template.version + 1}`}
            hint="The current version is kept and deactivated, so a message already sent can still be shown as the text that was actually sent."
            onConfirm={async (reason) => {
              setError(null);
              try {
                await reviseTemplate(template.id, {
                  body: draft,
                  ...(template.channel === "email" ? { subject } : {}),
                  reason,
                });
                onDone();
              } catch (err) {
                setError(messageOf(err));
                throw err;
              }
            }}
          />
        ) : (
          <span className="faint">unchanged</span>
        )}
      </div>
      {error ? <p className="danger">{error}</p> : null}
    </div>
  );
}

/** The earliest permitted effective date. Today is already too late. */
function tomorrow(): string {
  const d = new Date();
  d.setDate(d.getDate() + 1);
  return d.toISOString().slice(0, 10);
}
