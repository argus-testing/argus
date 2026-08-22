import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { createRequire } from "node:module";
import { join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const require = createRequire(import.meta.url);
const sourcePath = fileURLToPath(new URL("./mcpConfig.ts", import.meta.url));
const tempDirectory = mkdtempSync(join(tmpdir(), "argus-mcp-config-"));
const outputPath = join(tempDirectory, "mcpConfig.cjs");

try {
  const source = readFileSync(sourcePath, "utf8");
  const output = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText;
  writeFileSync(outputPath, output);
} catch (error) {
  rmSync(tempDirectory, { recursive: true, force: true });
  throw error;
}

const { getMcpConfig } = require(outputPath);

test.after(() => rmSync(tempDirectory, { recursive: true, force: true }));

test("generates a local stdio configuration with the Argus server URL for every supported client", () => {
  const expected = {
    mcpServers: {
      argus: {
        command: "uv",
        args: ["run", "argus-mcp"],
        env: { ARGUS_BASE_URL: "http://127.0.0.1:8000" },
      },
    },
  };
  for (const client of ["claude-code", "cursor", "generic"]) {
    assert.deepEqual(JSON.parse(getMcpConfig(client)), expected);
  }
});

test("does not include credentials or cloud endpoints", () => {
  const config = getMcpConfig("claude-code");
  assert.doesNotMatch(config, /key|token|https:\/\//i);
});
