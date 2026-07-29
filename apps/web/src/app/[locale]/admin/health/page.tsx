"use client";

import { useQuery } from "@tanstack/react-query";

import { Panel, plural } from "@/components/admin/detail";
import { Tag, since, waited } from "@/components/admin/table";
import { POLL_MS } from "@/components/admin/shell";
import { getHealth } from "@/lib/admin-api";

/**
 * Health — the page that exists because **a worker that stops is invisible.**
 *
 * `/healthz` returns ok whether or not anything is being processed. The API is
 * up, the database answers, and routes go unplanned until a customer rings to
 * ask where their driver is. Nothing in the system notices, because nothing is
 * watching for absence.
 *
 * Everything here detects absence rather than failure. Failures log; the failure
 * mode with no error message is "this stopped happening", and every panel below
 * is a way of seeing that.
 */

export default function HealthPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ["admin", "health"],
    queryFn: getHealth,
    refetchInterval: POLL_MS,
  });

  if (error) return <p className="danger">Health could not be loaded.</p>;
  if (isLoading || !data) return <p className="muted">Loading…</p>;

  const stale = data.loops.filter((l) => l.stale);
  const trapped = data.partitions.filter((p) => p.trapped);
  // An hour is the threshold where a backlog stops being throughput and starts
  // being a consumer that is not running.
  const laggy = data.outbox.filter((o) => (o.oldest_seconds ?? 0) > 3600);

  return (
    <div className="detail">
      <div className="detail-head">
        <div>
          <h2>
            Health
            {stale.length + trapped.length + laggy.length === 0 ? (
              <Tag tone="ok">nothing stalled</Tag>
            ) : (
              <Tag tone="danger">{stale.length + trapped.length + laggy.length} to look at</Tag>
            )}
          </h2>
          <p className="console-lede">
            What has stopped happening. As of {new Date(data.generated_at).toLocaleTimeString()},
            refreshed every {POLL_MS / 1000}s.
          </p>
        </div>
      </div>

      {trapped.length > 0 ? (
        <p className="alarm">
          {plural(trapped.length, "DEFAULT partition")} holding rows. Once a row lands in a DEFAULT
          partition, the range it occupies can never be attached as a real partition without moving
          it — so a partition job that ran late has become one that ran wrong. Fix the job, then
          move the rows.
        </p>
      ) : null}

      <Panel title="Background loops" note={plural(data.loops.length, "loop")}>
        <table className="money-table">
          <thead>
            <tr>
              <th>Loop</th>
              <th>Last did something</th>
              <th className="num">Waiting on it</th>
            </tr>
          </thead>
          <tbody>
            {data.loops.map((l) => (
              <tr key={l.loop}>
                <td>{l.loop}</td>
                <td className={l.stale ? "danger" : "faint"}>
                  {l.last_seen ? `${since(l.last_seen)} ago` : "never"}
                </td>
                <td className="num">
                  {l.pending > 0 ? (
                    l.stale ? (
                      <Tag tone="danger">{l.pending} stuck</Tag>
                    ) : (
                      <span>{l.pending}</span>
                    )
                  ) : (
                    <span className="faint">0</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {/* Said out loud because the reader will otherwise trust this more than
            it deserves. */}
        <p className="muted">
          Inferred from what each loop produces, not from a heartbeat — there is no job table yet. A
          loop with nothing to do produces nothing and looks identical to one that has stopped,
          which is why <strong>stuck</strong> requires both pending work and a quiet loop. Either
          alone is normal.
        </p>
      </Panel>

      <Panel title="Outbox" note={plural(data.outbox.length, "stream")}>
        {data.outbox.length === 0 ? (
          <p className="muted">Every event has been published.</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Stream</th>
                <th className="num">Unpublished</th>
                <th>Oldest</th>
                <th className="num">Retrying</th>
                <th>Last error</th>
              </tr>
            </thead>
            <tbody>
              {data.outbox.map((o) => (
                <tr key={o.aggregate_type}>
                  <td className="mono">{o.aggregate_type}</td>
                  <td className="num">{o.unpublished}</td>
                  <td className={(o.oldest_seconds ?? 0) > 3600 ? "danger" : "faint"}>
                    {o.oldest_seconds !== null ? `${waited(o.oldest_seconds)} ago` : "—"}
                  </td>
                  <td className={`num ${o.failing > 0 ? "danger" : "faint"}`}>{o.failing}</td>
                  <td className="faint">{o.last_error ?? "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Notifications, last 24h" note={plural(data.delivery.length, "channel")}>
        {data.delivery.length === 0 ? (
          <p className="muted">
            Nothing sent in 24 hours. On a live platform that is the alarm, not the all-clear.
          </p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Channel</th>
                <th className="num">Sent</th>
                <th className="num">Failed</th>
                <th className="num">Suppressed</th>
                <th className="num">Queued</th>
                <th>Last sent</th>
              </tr>
            </thead>
            <tbody>
              {data.delivery.map((d) => (
                <tr key={d.channel}>
                  <td>{d.channel}</td>
                  <td className="num">{d.sent}</td>
                  {/* Three columns, not one success rate. A failure is an
                      integration problem, a suppression is compliance working
                      correctly, and a queue that is not draining is a worker.
                      Three different people fix them. */}
                  <td className={`num ${d.failed > 0 ? "danger" : "faint"}`}>{d.failed}</td>
                  <td className="num faint">{d.suppressed}</td>
                  <td className={`num ${d.queued > 0 ? "danger" : "faint"}`}>{d.queued}</td>
                  <td className="faint">
                    {d.last_sent_at ? `${since(d.last_sent_at)} ago` : "never"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Partitions" note={plural(data.partitions.length, "partition")}>
        {data.partitions.length === 0 ? (
          <p className="muted">No partitioned tables.</p>
        ) : (
          <table className="money-table">
            <tbody>
              {data.partitions.map((p) => (
                <tr key={p.name} className={p.trapped ? "unattributed" : ""}>
                  <td className="mono">{p.name}</td>
                  <td className="faint">{p.parent}</td>
                  <td>
                    {p.is_default ? (
                      <Tag tone={p.rows > 0 ? "danger" : "warn"}>DEFAULT</Tag>
                    ) : (
                      <span className="faint">{p.bound.replace(/^FOR VALUES /, "")}</span>
                    )}
                  </td>
                  <td className="num faint">{p.rows.toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Moderation, by day" note={plural(data.moderation.length, "day")}>
        {data.moderation.length === 0 ? (
          <p className="muted">Nothing decided in this window.</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Day</th>
                <th className="num">Decided</th>
                <th className="num">Past SLA</th>
                <th className="num">Median wait</th>
              </tr>
            </thead>
            <tbody>
              {data.moderation.map((m) => (
                <tr key={m.day}>
                  <td>{new Date(m.day).toLocaleDateString()}</td>
                  <td className="num">{m.decided}</td>
                  {/* A rate over days, not a count right now. "We are behind
                      today" and "we have been behind for a fortnight" look
                      identical on a queue screen and are a bad morning versus a
                      staffing decision. */}
                  <td className={`num ${m.breached > 0 ? "danger" : "faint"}`}>
                    {m.breached}
                    {m.decided > 0 ? (
                      <span className="faint"> ({Math.round((m.breached / m.decided) * 100)}%)</span>
                    ) : null}
                  </td>
                  <td className="num faint">
                    {m.median_seconds !== null ? waited(Math.round(m.median_seconds)) : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
    </div>
  );
}
