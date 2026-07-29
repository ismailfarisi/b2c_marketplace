"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { Action, Panel, plural } from "@/components/admin/detail";
import { Tag, since } from "@/components/admin/table";
import { getFlags, getSettings, setFlag, setSetting, type AppSetting } from "@/lib/admin-api";

/**
 * Configuration — the last routine reason to open `psql` against production.
 *
 * Two tables, both created for this page. Every change takes a reason and writes
 * an audit row in the same transaction as the change itself, which is the §3.3
 * rule honoured properly rather than approximately: no payment gateway sits in
 * the middle here, so there is no excuse for the two to commit separately.
 *
 * Notably **not** here: tax rates and boost rate cards. Both carry a `daterange`
 * with an exclusion constraint, and changing one correctly means closing the
 * current row and opening a new one — a version, not an edit. A form that let
 * someone change a VAT rate in place would silently restate every invoice
 * already issued under the old one, and the fact that it would be easy to build
 * is exactly why it is dangerous.
 */

export default function ConfigPage() {
  const queryClient = useQueryClient();
  const flags = useQuery({ queryKey: ["admin", "flags"], queryFn: getFlags });
  const settings = useQuery({ queryKey: ["admin", "settings"], queryFn: getSettings });

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["admin", "flags"] });
    void queryClient.invalidateQueries({ queryKey: ["admin", "settings"] });
  };

  if (flags.error) {
    return (
      <p className="danger">
        Configuration could not be loaded. If this reads &ldquo;forbidden&rdquo;, the account is
        missing <span className="mono">health.read</span>.
      </p>
    );
  }

  return (
    <div className="detail">
      <div className="detail-head">
        <div>
          <h2>Configuration</h2>
          <p className="console-lede">
            Changing anything here needs a stated reason, and the reason is recorded against your
            name in the same transaction as the change.
          </p>
        </div>
      </div>

      <Panel title="Feature flags" note={plural(flags.data?.items.length ?? 0, "flag")}>
        {(flags.data?.items ?? []).length === 0 ? (
          <p className="muted">
            No flags declared. Flags are created by a migration rather than from this page, so that
            a typo cannot bring one into existence and quietly gate nothing.
          </p>
        ) : (
          <table className="money-table">
            <tbody>
              {flags.data!.items.map((f) => (
                <tr key={f.key}>
                  <td>
                    <span className="mono">{f.key}</span>
                    <div className="faint">{f.description}</div>
                  </td>
                  <td>
                    <Tag tone={f.enabled ? "ok" : undefined}>{f.enabled ? "on" : "off"}</Tag>
                    {f.enabled && f.rollout_pct < 100 ? (
                      <span className="faint"> {f.rollout_pct}%</span>
                    ) : null}
                  </td>
                  <td className="faint">
                    {f.updated_by ? `${f.updated_by}, ${since(f.updated_at)} ago` : "never changed"}
                  </td>
                  <td>
                    <Action
                      label={f.enabled ? "Turn off" : "Turn on"}
                      verb={f.enabled ? "Turn it off" : "Turn it on"}
                      danger={f.enabled}
                      hint={
                        f.enabled
                          ? "Turning a flag off during an incident is the right call and the reason still matters — it is what the post-mortem reads."
                          : undefined
                      }
                      onConfirm={async (reason) => {
                        await setFlag(f.key, {
                          enabled: !f.enabled,
                          rollout_pct: f.enabled ? f.rollout_pct : 100,
                          reason,
                        });
                        invalidate();
                      }}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Settings" note={plural(settings.data?.items.length ?? 0, "setting")}>
        {settings.isLoading ? (
          <p className="muted">Loading…</p>
        ) : (
          <table className="money-table">
            <tbody>
              {(settings.data?.items ?? []).map((s) => (
                <SettingRow key={s.key} setting={s} onSaved={invalidate} />
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Not editable here">
        <p className="muted">
          <strong>Tax rates</strong> and <strong>boost rate cards</strong> are date-ranged with an
          exclusion constraint. Changing one means closing the current row and opening a new one in
          the same transaction — a version, not an edit. Editing in place would silently restate
          every invoice already issued under the old rate, so it is done by migration until there is
          a form that understands versions.
        </p>
        <p className="muted">
          <strong>Notification templates</strong> are versioned per locale and channel. The same
          argument applies, with the addition that an Arabic SMS body crossing a segment boundary
          changes what a send costs — so the editor needs to show that before it can safely exist.
        </p>
      </Panel>
    </div>
  );
}

function SettingRow({ setting, onSaved }: { setting: AppSetting; onSaved: () => void }) {
  const [draft, setDraft] = useState(JSON.stringify(setting.value));

  return (
    <tr>
      <td>
        <span className="mono">{setting.key}</span>
        <div className="faint">{setting.description}</div>
      </td>
      <td className="mono">
        {/* A redacted value is shown as redacted rather than as an empty cell.
            "This has no value" and "you may not see this value" are different
            facts and only one of them is a reason to go looking. */}
        {setting.sensitive && setting.value === "«redacted»" ? (
          <Tag tone="warn">hidden</Tag>
        ) : (
          JSON.stringify(setting.value)
        )}
      </td>
      <td className="faint">
        {setting.updated_by ? `${setting.updated_by}, ${since(setting.updated_at)} ago` : "default"}
      </td>
      <td>
        <Action
          label="Change"
          verb="Save this value"
          hint="JSON — a bare string needs quotes."
          extra={(busy) => (
            <label className="field">
              <span>New value</span>
              <input
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                disabled={busy}
                className="mono"
              />
            </label>
          )}
          onConfirm={async (reason) => {
            // Parsed here so an invalid value is refused before it is sent,
            // with a message about JSON rather than a 422 about a field the
            // operator did not know was a field.
            let parsed: unknown;
            try {
              parsed = JSON.parse(draft);
            } catch {
              throw new Error(`${draft} is not valid JSON — a bare string needs quotes.`);
            }
            await setSetting(setting.key, { value: parsed, reason });
            onSaved();
          }}
        />
      </td>
    </tr>
  );
}
