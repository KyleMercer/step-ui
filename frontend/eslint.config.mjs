// Next.js 16 / ESLint 10 flat config. `eslint-config-next` ships a native
// flat-config array (see node_modules/eslint-config-next/dist/*.js), so we
// import it directly rather than bridging the old string-based
// "next/core-web-vitals" extends through @eslint/eslintrc's FlatCompat —
// that compatibility shim breaks under ESLint 10 with a
// "Converting circular structure to JSON" error when it tries to re-wrap an
// already-flat plugin object.
import nextCoreWebVitals from "eslint-config-next/core-web-vitals";

const eslintConfig = [
  ...nextCoreWebVitals,
  {
    rules: {
      // This app fetches its initial data with a plain "call an async
      // loader from useEffect(..., [])" pattern on every page (Dashboard,
      // Inventory, Settings) -- the standard fetch-on-mount pattern for a
      // client-rendered Next.js page, not using Server Components/Suspense
      // data fetching. It's functionally correct (React always finishes
      // mounting before an effect runs, so there's no real race), but this
      // rule -- new in this ESLint/React Compiler-era version bump --
      // flags it anyway as part of a broader push toward
      // Suspense-based data fetching. Migrating every page to that model
      // is a real, separate architectural change, not something to fold
      // silently into a dependency-version bump, so this rule is turned
      // off here rather than either working around it with something
      // more convoluted or restructuring data fetching app-wide.
      "react-hooks/set-state-in-effect": "off",
    },
  },
];

export default eslintConfig;
