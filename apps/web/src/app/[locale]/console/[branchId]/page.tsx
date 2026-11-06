"use client";

import { formatMoney } from "@marketplace/api-client";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";

import { Panel } from "@/components/admin/detail";
import { label } from "@/components/kitchen/copy";
import { ConsoleError, useBranch } from "@/components/kitchen/portal";
import { getSummary } from "@/lib/api";
import { branchToday, getOverview } from "@/lib/vendor-api";

/**
 * Where a kitchen starts its day, and — for a new kitchen — the setup
 * checklist. A kitchen is not sellable until it has a profile, dishes, a plan
 * and a menu that covers every day that plan delivers; the checklist says which
 * of those is missing and links straight to the fix.
 */
export default function OverviewPage() {
  const branch = useBranch();
  const { t, locale } = branch;
  const c = t.overview;
  const date = branchToday(branch.timezone);
  const overview = useQuery({
    queryKey: ["vendor", "overview", branch.branch_id],
    queryFn: () => getOverview(branch.branch_id),
  });
  const today = useQuery({
    queryKey: ["summary", branch.branch_id, date],
    queryFn: () => getSummary(branch.branch_id, date),
    refetchInterval: 30_000,
  });

  if (overview.error) {
    return (
      <ConsoleError
        error={overview.error}
        onRetry={() => void overview.refetch()}
        locale={locale}
      />
    );
  }
  const o = overview.data;
  const s = today.data;

  const steps: Array<{ done: boolean; label: string; href: string }> = o
    ? [
        { done: o.profile_complete, label: c.stepProfile, href: "/profile" },
        { done: o.active_dishes > 0, label: c.stepDishes, href: "/dishes" },
        { done: o.active_plans > 0, label: c.stepPlan, href: "/plans" },
        {
          done: o.active_plans > 0 && o.menu_gaps.length === 0,
          label:
            o.menu_gaps.length > 0
              ? c.stepGaps(o.menu_gaps.length)
              : c.stepMenu,
          href: "/menu",
        },
      ]
    : [];
  const setupDone = steps.every((s) => s.done);

  return (
    <>
      <div className="stats">
        <Stat k={c.activeSubscribers} v={o?.active_subscribers} />
        <Stat k={c.paused} v={o?.paused_subscribers} />
        <Stat k={c.awaitingPayment} v={o?.pending_payment} />
        <Stat
          k={c.livePlans}
          v={o?.active_plans}
          sub={o ? c.inRotation(o.rotation_plans) : undefined}
        />
        <Stat k={c.dishes} v={o?.active_dishes} />
      </div>

      {o && !setupDone ? (
        <Panel
          title={c.getReady}
          note={c.doneOf(steps.filter((s) => s.done).length, steps.length)}
        >
          <ul className="vp-checklist">
            {steps.map((s) => (
              <li key={s.href} className={s.done ? "done" : "todo"}>
                <span className="mark">{s.done ? "✓" : "•"}</span>
                {s.done ? (
                  <span>{s.label}</span>
                ) : (
                  <Link href={`${branch.base}${s.href}`}>{s.label}</Link>
                )}
              </li>
            ))}
          </ul>
        </Panel>
      ) : null}

      {o && o.active_subscribers > 0 && o.menu_weeks <= 1 ? (
        <Panel title={t.menuWeeks.nudgeTitle}>
          <p>
            {t.menuWeeks.nudge}{" "}
            <Link href={`${branch.base}/menu`}>{t.menuWeeks.nudgeLink}</Link>
          </p>
        </Panel>
      ) : null}

      {o && o.menu_gaps.length > 0 ? (
        <Panel title={c.menuGaps} note={c.menuGapsNote}>
          <div className="vp-chips">
            {o.menu_gaps.map((g) => (
              <Link
                key={`${g.week}-${g.weekday}-${g.slot}`}
                className="tag warn"
                href={`${branch.base}/menu`}
              >
                {o.menu_weeks > 1 ? `${t.menuWeeks.week(g.week)} · ` : ""}
                {t.weekdays[g.weekday]} {label(t.slots, g.slot)}
              </Link>
            ))}
          </div>
        </Panel>
      ) : null}

      <Panel
        title={c.today}
        note={
          <Link href={`${branch.base}/deliveries`}>{c.openDeliveries}</Link>
        }
      >
        {today.error ? (
          <ConsoleError
            error={today.error}
            onRetry={() => void today.refetch()}
            locale={locale}
          />
        ) : (
          <div className="stats">
            <Stat k={c.scheduled} v={s?.scheduled} />
            <Stat k={c.inProgress} v={s?.in_progress} />
            <Stat k={c.delivered} v={s?.completed} />
            <Stat k={c.failed} v={s?.failed} breached={!!s?.failed} />
            <Stat k={c.noDriver} v={s?.unassigned} breached={!!s?.unassigned} />
            <Stat
              k={c.payable}
              v={
                s
                  ? formatMoney(
                      { amount: s.payable_minor, currency: s.currency },
                      locale === "ar" ? "ar" : "en",
                    )
                  : undefined
              }
            />
          </div>
        )}
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
