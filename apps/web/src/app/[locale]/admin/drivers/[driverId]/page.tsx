"use client";

import { useQuery } from "@tanstack/react-query";
import { use } from "react";

import { Detail, Facts, Panel, Ref, TimelinePanel, plural } from "@/components/admin/detail";
import { Tag, money, since } from "@/components/admin/table";
import { getDriverDetail, getDriverTimeline } from "@/lib/admin-api";

/**
 * The driver page.
 *
 * Opened with one of two questions — "where is my delivery" or "why is this
 * person holding four hundred dirhams" — so position and cash come first and the
 * paperwork comes last.
 *
 * Position and its age are shown as separate facts. A driver with no position at
 * all has not opened the app; one with a position from three hours ago has the
 * app open and is not reporting. Both look like a missing pin on a dispatch map
 * and need different phone calls, and collapsing them into "last seen" loses
 * exactly that.
 */

export default function DriverPage({
  params,
}: {
  params: Promise<{ locale: string; driverId: string }>;
}) {
  const { locale, driverId } = use(params);

  const { data, isLoading, error } = useQuery({
    queryKey: ["admin", "driver", driverId],
    queryFn: () => getDriverDetail(driverId),
    refetchInterval: 30_000,
  });

  if (error) return <p className="danger">This driver could not be loaded.</p>;
  if (isLoading || !data) return <p className="muted">Loading driver…</p>;

  const expiredDocs = data.documents.filter((d) => d.expired);
  const pingAge = data.last_ping_at ? ageMinutes(data.last_ping_at) : null;
  const goneQuiet = data.shift.on && (pingAge === null || pingAge > 20);

  return (
    <Detail
      title={data.name}
      subtitle={`${data.vehicle_type}${data.plate_no ? ` · ${data.plate_no}` : ""} · joined ${new Date(
        data.created_at,
      ).toLocaleDateString()}`}
      tags={
        <>
          <Tag tone={data.status === "active" ? "ok" : "warn"}>{data.status}</Tag>
          {data.shift.on ? <Tag tone="ok">on shift</Tag> : <Tag>off shift</Tag>}
          {expiredDocs.length > 0 ? (
            <Tag tone="danger">{plural(expiredDocs.length, "expired document")}</Tag>
          ) : null}
        </>
      }
      aside={
        <TimelinePanel
          queryKey={["admin", "driver", driverId, "timeline"]}
          fetcher={() => getDriverTimeline(driverId)}
        />
      }
    >
      {goneQuiet ? (
        <p className="alarm">
          On shift and not reporting. The last position was{" "}
          {data.last_ping_at ? `${since(data.last_ping_at)} ago` : "never recorded"}, so the
          dispatch map is showing something stale.{" "}
          {data.workload.open_stops > 0
            ? `${plural(data.workload.open_stops, "stop")} still assigned.`
            : "No stops assigned, so nothing is stranded."}
        </p>
      ) : null}

      <Panel title="Where they are">
        <Facts
          rows={[
            [
              "Last position",
              data.position ? (
                <span className="mono">
                  {data.position.lat.toFixed(5)}, {data.position.lng.toFixed(5)}
                </span>
              ) : (
                // Distinct from a stale position, and said in words rather
                // than left as an empty cell.
                <Tag tone="warn">never reported</Tag>
              ),
            ],
            [
              "Last heard from",
              data.last_ping_at ? (
                <span className={pingAge !== null && pingAge > 20 ? "danger" : ""}>
                  {since(data.last_ping_at)} ago
                </span>
              ) : null,
            ],
            [
              "Shift",
              data.shift.on ? (
                <>
                  since {data.shift.from ? new Date(data.shift.from).toLocaleTimeString() : "—"}
                  {data.shift.until
                    ? ` until ${new Date(data.shift.until).toLocaleTimeString()}`
                    : ""}
                </>
              ) : null,
            ],
            [
              "Works for",
              data.vendor && data.vendor_id ? (
                <Ref locale={locale} kind="vendors" id={data.vendor_id}>
                  {data.vendor}
                </Ref>
              ) : (
                <span className="faint">the platform</span>
              ),
            ],
            ["Phone", data.phone ? <span className="mono">{data.phone}</span> : null],
            ["Capacity", `${data.max_stops} stops`],
          ]}
        />
      </Panel>

      <Panel title="Cash they are holding" note={data.cash.stops > 0 ? "unremitted" : undefined}>
        <Facts
          rows={[
            [
              "Held",
              <strong className={data.cash.held.amount > 0 ? "" : "faint"}>
                {money(data.cash.held)}
              </strong>,
            ],
            ["Across", data.cash.stops > 0 ? plural(data.cash.stops, "collection") : null],
            [
              "Oldest",
              data.cash.oldest ? (
                <span className={ageMinutes(data.cash.oldest) > 2880 ? "danger" : ""}>
                  {since(data.cash.oldest)} ago
                </span>
              ) : null,
            ],
          ]}
        />
        {data.cash.held.amount > 0 ? (
          <p className="muted">
            This is the platform&rsquo;s money in someone else&rsquo;s pocket. It is not a shortfall
            until it is declared and counted short — the reconciliation screen is where that
            comparison happens.
          </p>
        ) : null}
      </Panel>

      <Panel title="Today">
        <Facts
          rows={[
            ["Open stops", data.workload.open_stops],
            ["Completed", data.workload.completed_today],
            [
              "Failed",
              data.workload.failed_today > 0 ? (
                <Tag tone="warn">{data.workload.failed_today}</Tag>
              ) : (
                0
              ),
            ],
          ]}
        />
      </Panel>

      <Panel title="Documents" note={plural(data.documents.length, "document")}>
        {data.documents.length === 0 ? (
          <p className="muted">
            Nothing on file. A driver with no verified licence should not be taking stops, and this
            is the only place that is visible.
          </p>
        ) : (
          <table className="money-table">
            <tbody>
              {data.documents.map((d) => (
                <tr key={d.kind}>
                  <td className="mono">{d.kind}</td>
                  <td>
                    <Tag tone={d.state === "verified" ? "ok" : "warn"}>{d.state}</Tag>
                  </td>
                  <td className={d.expired ? "danger" : "faint"}>
                    {d.expires_on
                      ? d.expired
                        ? `expired ${d.expires_on}`
                        : `until ${d.expires_on}`
                      : "no expiry recorded"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
    </Detail>
  );
}

function ageMinutes(iso: string): number {
  return (Date.now() - new Date(iso).getTime()) / 60_000;
}
