import { Tag } from "@/components/admin/table";

/** datetime-local value in the browser's zone, from an ISO instant. */
export function toLocalInput(iso: string | null) {
  if (!iso) return "";
  const d = new Date(iso);
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 16);
}

export function fromLocalInput(v: string) {
  return v ? new Date(v).toISOString() : null;
}

export function windowState(b: {
  is_active: boolean;
  starts_at: string | null;
  ends_at: string | null;
}) {
  const now = Date.now();
  if (!b.is_active) return <Tag>off</Tag>;
  if (b.starts_at && Date.parse(b.starts_at) > now)
    return <Tag tone="warn">scheduled</Tag>;
  if (b.ends_at && Date.parse(b.ends_at) <= now) return <Tag>ended</Tag>;
  return <Tag tone="ok">live</Tag>;
}
