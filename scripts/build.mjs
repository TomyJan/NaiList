import { spawnSync } from "node:child_process";
import { buildVersion, projectRoot } from "./build-version.mjs";

const version = buildVersion();
const result = spawnSync(
  "go",
  [
    "build",
    "-trimpath",
    "-ldflags",
    `-s -w -X main.buildVersion=${version}`,
    ...process.argv.slice(2),
    "./cmd/server",
  ],
  { cwd: projectRoot, stdio: "inherit" },
);
if (result.error) throw result.error;
process.exit(result.status ?? 1);
