#!/usr/bin/env node
/**
 * Fails if regenerating clients from api/openapi.yaml produces a diff.
 *
 * The contract is only a source of truth if drift is impossible. A spec change that
 * ships without regenerated clients is the same class of bug as a schema migration
 * that ships without the code that uses it — it just fails later and further away.
 */
import { execFileSync } from "node:child_process";

const run = (cmd, args) =>
  execFileSync(cmd, args, { stdio: "inherit", shell: process.platform === "win32" });

const capture = (cmd, args) =>
  execFileSync(cmd, args, { encoding: "utf8", shell: process.platform === "win32" }).trim();

const generated = [
  "packages/api-client/src/gen",
  "services/api/internal/gen",
];

console.log("→ regenerating TypeScript client");
run("npx", ["orval", "--config", "api/orval.config.ts"]);

console.log("→ regenerating Go server boundary");
// The oapi-codegen tool directive lives in services/api/go.mod, so run from
// there (as `make gen-go` does); from the repo root there is no module.
let haveGo = true;
try {
  capture("go", ["version"]);
} catch {
  haveGo = false;
  console.error(
    "! go toolchain unavailable — skipping the Go half of this check.\n" +
      "  CI must run with Go installed or this check is only half a check.",
  );
}
if (haveGo) {
  run("go", ["tool", "-C", "services/api", "oapi-codegen", "-config", "../../api/oapi-codegen.yaml", "../../api/openapi.yaml"]);
}

const diff = capture("git", ["status", "--porcelain", "--", ...generated]);
if (diff) {
  console.error("\n✗ Generated code is out of date with api/openapi.yaml:\n");
  console.error(diff);
  console.error("\n  Run `npm run gen:ts` and `make gen-go`, then commit the result.\n");
  process.exit(1);
}

console.log("✓ generated clients match the spec");
