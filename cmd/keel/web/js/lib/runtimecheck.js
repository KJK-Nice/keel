// Is the keel serve we're talking to from the same build as the app window?
// Pure function: no DOM, no state.
//
// The app passes its build id as ?app_build=; /api/health reports the
// runtime's. A runtime from before build ids existed reports none, and is
// older by definition. A page opened without app_build (a plain browser,
// tests) never warns.
export function runtimeIsStale(appBuild, runtimeBuild) {
  if (!appBuild) return false;
  return appBuild !== (runtimeBuild || "");
}
