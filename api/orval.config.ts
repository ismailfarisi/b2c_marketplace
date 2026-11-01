import { defineConfig } from "orval";

/**
 * Generates the TypeScript client from api/openapi.yaml.
 *   npm run gen:ts
 *
 * **Web only.** Mobile is Kotlin Multiplatform and generates from the same
 * `openapi.yaml` through a separate Kotlin target — see apps/mobile-kmm/README.md
 * §4. That makes the spec the actual cross-language contract rather than a
 * description of one, and it is why spec drift has to fail CI: it is no longer
 * caught by a shared TypeScript type.
 *
 * The web app does not hand-write a fetch call against this API — if an endpoint
 * is missing from the client, the fix is to add it to the spec.
 */
export default defineConfig({
  marketplace: {
    input: {
      target: "./openapi.yaml",
    },
    output: {
      mode: "tags-split",
      target: "../packages/api-client/src/gen/endpoints",
      schemas: "../packages/api-client/src/gen/models",
      client: "react-query",
      clean: true,
      prettier: false,
      override: {
        mutator: {
          path: "../packages/api-client/src/http-client.ts",
          name: "apiFetch",
        },
        query: {
          useQuery: true,
          // Not every list takes a cursor; infinite hooks for those don't typecheck.
          useInfinite: false,
        },
      },
    },
  },
});
