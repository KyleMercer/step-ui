// Next.js 16 / ESLint 10 flat config. `eslint-config-next` ships a native
// flat-config array (see node_modules/eslint-config-next/dist/*.js), so we
// import it directly rather than bridging the old string-based
// "next/core-web-vitals" extends through @eslint/eslintrc's FlatCompat —
// that compatibility shim breaks under ESLint 10 with a
// "Converting circular structure to JSON" error when it tries to re-wrap an
// already-flat plugin object.
import nextCoreWebVitals from "eslint-config-next/core-web-vitals";

const eslintConfig = [...nextCoreWebVitals];

export default eslintConfig;
