"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { Panel } from "@/components/admin/detail";
import { getFoodSafety, setFoodSafety } from "@/lib/admin-api";

/** Staff record a checked food-safety licence; the storefront badge lapses with it. */
export function FoodSafetyPanel({ branchId }: { branchId: string }) {
  const qc = useQueryClient();
  const key = ["admin", "food-safety", branchId];
  const { data } = useQuery({ queryKey: key, queryFn: () => getFoodSafety(branchId) });
  const [ref, setRef] = useState("");
  const [expires, setExpires] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    setRef(data?.ref ?? "");
    setExpires(data?.expires_on ?? "");
  }, [data]);
  const save = useMutation({
    mutationFn: (verified: boolean) => setFoodSafety(branchId, { verified, ref, expires_on: expires }),
    onSuccess: async () => {
      setProblem(null);
      await qc.invalidateQueries({ queryKey: key });
    },
    onError: (e: Error) => setProblem(e.message),
  });
  return (
    <Panel title="Food safety" note={data?.verified ? `verified · expires ${data.expires_on}` : "not verified"}>
      <form
        className="merch-form"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate(true);
        }}
      >
        <label className="field">
          <span>Licence / certificate number</span>
          <input value={ref} onChange={(e) => setRef(e.target.value)} />
        </label>
        <label className="field">
          <span>Expires on</span>
          <input type="date" value={expires} onChange={(e) => setExpires(e.target.value)} />
        </label>
        {problem && <p className="danger">{problem}</p>}
        <div className="merch-actions">
          <button className="btn primary" type="submit" disabled={save.isPending}>
            {data?.verified ? "Update verification" : "Mark verified"}
          </button>
          {data?.verified && (
            <button
              className="btn danger"
              type="button"
              onClick={() => window.confirm("Remove the food-safety badge?") && save.mutate(false)}
            >
              Remove verification
            </button>
          )}
        </div>
      </form>
    </Panel>
  );
}
