import { join } from "node:path";
import { pathToFileURL } from "node:url";

const sdk = await import(pathToFileURL(join(process.argv[1], "@earendil-works/pi-coding-agent/dist/index.js")).href);
const { createAgentSession, DefaultResourceLoader, getAgentDir, SessionManager } = sdk;
const cwd = process.cwd();
const resourceLoader = new DefaultResourceLoader({
  cwd,
  agentDir: getAgentDir(),
  noExtensions: true,
  noSkills: true,
  noPromptTemplates: true,
});
const { session } = await createAgentSession({
  cwd,
  tools: ["read", "grep", "find", "ls"],
  resourceLoader,
  sessionManager: SessionManager.inMemory(cwd),
});
try {
  let prompt = "";
  for await (const chunk of process.stdin) prompt += chunk;
  await session.prompt(prompt);
  const last = session.messages.findLast((message) => message.role === "assistant");
  if (!last || last.stopReason !== "stop") throw new Error(`Pi did not finish: ${last?.stopReason ?? "no response"}`);
  process.stdout.write(session.getLastAssistantText() ?? "");
} finally {
  session.dispose();
}
