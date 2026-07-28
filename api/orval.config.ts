import { defineConfig } from "orval";

/**
 * Generates the TypeScript client from api/openapi.yaml.
 *   npm run gen:ts
 *
 * Web and mobile both consume `@marketplace/api-client`. Neither app hand-writes a
 * fetch call against this API — if an endpoint is missing from the client, the fix
 * is to add it to the spec.
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
      httpClient: "fetch",
      clean: true,
      prettier: false,
      override: {
        mutator: {
          path: "../packages/api-client/src/http-client.ts",
          name: "apiFetch",
        },
        query: {
          useQuery: true,
          useInfinite: true,
          useInfiniteQueryParam: "cursor",
        },
      },
    },
  },
});
