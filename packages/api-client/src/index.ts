export { apiFetch, configureApiClient, ApiError } from "./http-client";
export type { ApiProblem, TokenStore } from "./http-client";

export * from "./money";

// Generated surface. These paths exist only after `npm run gen:ts`, which is
// why they are re-exported from one place: nothing outside this package should
// ever import a path under src/gen directly.
//
// export * from "./gen/endpoints";
// export * from "./gen/models";
