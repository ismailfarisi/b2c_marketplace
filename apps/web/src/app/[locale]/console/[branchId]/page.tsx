"use client";

import { formatMoney } from "@marketplace/api-client";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";

import { Panel } from "@/components/admin/detail";
import { errorText, useBranch } from "@/components/kitchen/portal";
import { getSummary } from "@/lib/api";
import { getOverview, WEEKDAYS } from "@/lib/vendor-api";

/**
 * Where a kitchen starts its day, and — for a new kitchen — the setup
 * checklist. A kitchen is not sellable until it has a profile, dishes, a plan
 * and a menu that covers every day that plan delivers; the checklist says which
 * of those is missing and links straight to the fix.
 */
export default function OverviewPage() {
  const branch = useBranch();
  const overview = useQuery({
    queryKey: ["vendor", "overview", branch.branch_id],
    queryFn: () => getOverview(branch.branch_id),
  });
  const today = useQuery({
    queryKey: ["summary", branch.branch_id, "today"],
    queryFn: () => getSummary(branch.branch_id),
    refetchInterval: 30_000,
  });

  if (overview.error) {
    return <div className="state error">{errorText(overview.error)}</div>;
  }
  const o = overview.data;
  const t = today.data;

  const steps: Array<{ done: boolean; label: string; href: string }> = o
    ? [
        { done: o.profile_complete, label: "Describe your kitchen and its cuisines", href: "/profile" },
        { done: o.active_dishes > 0, label: "Add the dishes you cook", href: "/dishes" },
        { done: o.active_plans > 0, label: "Create a plan customers can subscribe to", href: "/plans" },
        {
          done: o.active_plans > 0 && o.menu_gaps.length === 0,
          label:
            o.menu_gaps.length > 0
              ? `Fill ${o.menu_gaps.length} empty menu slot${o.menu_gaps.length === 1 ? "" : "s"}`
              : "Set the weekly menu",
          href: "/menu",
        },
      ]
    : [];
  const setupDone = steps.every((s) => s.done);

  return (
    <>
      <div className="stats">
        <Stat k="Active subscribers" v={o?.active_subscribers} />
        <Stat k="Paused" v={o?.paused_subscribers} />
        <Stat k="Awaiting payment" v={o?.pending_payment} />
        <Stat k="Live plans" v={o?.active_plans} sub={o ? `${o.rotation_plans} in rotation` : undefined} />
        <Stat k="Dishes" v={o?.active_dishes} />
      </div>

      {o && !setupDone ? (
        <Panel title="Get ready to sell" note={`${steps.filter((s) => s.done).length} of ${steps.length} done`}>
          <ul className="vp-checklist">
            {steps.map((s) => (
              <li key={s.href} className={s.done ? "done" : "todo"}>
                <span className="mark">{s.done ? "✓" : "•"}</span>
                {s.done ? <span>{s.label}</span> : <Link href={`${branch.base}${s.href}`}>{s.label}</Link>}
              </li>
            ))}
          </ul>
        </Panel>
      ) : null}

      {o && o.menu_gaps.length > 0 ? (
        <Panel title="Menu gaps" note="A live plan delivers these meals but the menu has no dish">
          <div className="vp-chips">
            {o.menu_gaps.map((g) => (
              <Link key={`${g.weekday}-${g.slot}`} className="tag warn" href={`${branch.base}/menu`}>
                {WEEKDAYS[g.weekday]} {g.slot}
              </Link>
            ))}
          </div>
        </Panel>
      ) : null}

      <Panel title="Today" note={<Link href={`${branch.base}/deliveries`}>Open deliveries</Link>}>
        <div className="stats">
          <Stat k="Scheduled" v={t?.scheduled} />
          <Stat k="In progress" v={t?.in_progress} />
          <Stat k="Delivered" v={t?.completed} />
          <Stat k="Failed" v={t?.failed} breached={!!t?.failed} />
          <Stat k="No driver" v={t?.unassigned} breached={!!t?.unassigned} />
          <Stat
            k="Payable to you"
            v={t ? formatMoney({ amount: t.payable_minor, currency: t.currency }, "en") : undefined}
          />
        </div>
      </Panel>
    </>
  );
}

function Stat({
  k,
  v,
  sub,
  breached,
}: {
  k: string;
  v: number | string | undefined;
  sub?: string | undefined;
  breached?: boolean;
}) {
  return (
    <div className={`stat ${breached ? "breached" : ""}`}>
      <div className="k">{k}</div>
      <div className="v">{v ?? "—"}</div>
      {sub ? <div className="sub">{sub}</div> : null}
    </div>
  );
}
