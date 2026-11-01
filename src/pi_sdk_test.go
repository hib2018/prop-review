package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPiSDKUTF8Chunks(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for the Pi SDK input regression test")
	}
	modules := t.TempDir()
	sdkDir := filepath.Join(modules, "@earendil-works", "pi-coding-agent", "dist")
	if err := os.MkdirAll(sdkDir, 0700); err != nil {
		t.Fatal(err)
	}
	const sdk = `
import assert from "node:assert/strict";
export class DefaultResourceLoader { async reload() {} }
export const getAgentDir = () => "";
export const SessionManager = { inMemory() {} };
export async function createAgentSession() {
  return { session: {
    async prompt(text) {
      assert.equal(text, globalThis.expectedPrompt, globalThis.chunkCase);
      globalThis.promptCalls++;
    },
    messages: [{ role: "assistant", stopReason: "stop" }],
    getLastAssistantText() { return ""; },
    dispose() {},
  } };
}
`
	if err := os.WriteFile(filepath.Join(sdkDir, "index.js"), []byte(sdk), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sdkDir, "package.json"), []byte(`{"type":"module"}`), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := json.Marshal(piSDK)
	if err != nil {
		t.Fatal(err)
	}
	script := `
import assert from "node:assert/strict";
import { Readable } from "node:stream";
import { setImmediate } from "node:timers/promises";
const source = ` + string(source) + `;
let cases = 0;
globalThis.promptCalls = 0;
for (const text of ["日本語", "ASCII: 日本語 mixed\n"]) {
  const bytes = Buffer.from(text);
  const splits = [[bytes], Array.from(bytes, byte => Buffer.from([byte]))];
  for (let i = 1; i < bytes.length; i++) {
    if ((bytes[i] & 0xc0) === 0x80) splits.push([bytes.subarray(0, i), bytes.subarray(i)]);
  }
  for (const chunks of splits) {
    globalThis.expectedPrompt = text;
    globalThis.chunkCase = JSON.stringify({ text, sizes: chunks.map(chunk => chunk.length) });
    // Yield separately so buffering cannot hide a split UTF-8 character.
    const input = Readable.from((async function* () {
      for (const chunk of chunks) {
        yield chunk;
        await setImmediate();
      }
    })(), { objectMode: false });
    Object.defineProperty(process, "stdin", { value: input, configurable: true });
    await import("data:text/javascript;base64," + Buffer.from(source).toString("base64") + "#" + cases++);
  }
}
assert.equal(globalThis.promptCalls, cases);
`
	cmd := exec.Command(node, "--input-type=module", "-e", script, modules)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Pi SDK UTF-8 chunks: %v\n%s", err, out)
	}
}
