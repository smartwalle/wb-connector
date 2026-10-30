// 校验 workbuddy/connector/ 目录能否被 WorkBuddy 的插件解析器接受。
//
// 校验逻辑逐条摘自 WorkBuddy 安装包中的实现：
//   parseAgentPluginManifest  —— .codebuddy-plugin/plugin.json
//   parseAgentPluginMcp       —— mcp.json
//   parseAgentPluginRemoteUrl  —— remote URL 规则
//   parseInsecureHttpMcpServers —— 非 HTTPS 的 HTTP 声明规则
import { readFileSync, existsSync } from "node:fs";
import { isIP } from "node:net";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

// 默认校验脚本同级的 connector/ 目录，与调用位置无关。
const SCRIPT_DIR = dirname(fileURLToPath(import.meta.url));
const PACKAGES = process.argv[2] ?? join(SCRIPT_DIR, "..", "connector");

const isPluginRecord = (e) => typeof e === "object" && e !== null && !Array.isArray(e);
const PLUGIN_FIELDS = new Set(["$schema", "name", "version", "description", "keywords", "author", "extensions"]);
const STDIO_FIELDS = new Set(["type", "command", "args", "env", "cwd"]);
const REMOTE_FIELDS = new Set(["type", "url", "headers"]);
const HEADER_NAME_RE = /^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/;

const problems = [];
const check = (ok, msg) => {
  console.log(`  ${ok ? "✓" : "✗"} ${msg}`);
  if (!ok) problems.push(msg);
};

function parseAgentPluginRemoteUrl(raw) {
  if (!/^https?:\/\/[^/?#]+/i.test(raw)) throw new Error("invalid remote URL");
  const u = new URL(raw);
  if (u.username || u.password) throw new Error("invalid remote URL");
  const loopback =
    u.hostname === "localhost" ||
    u.hostname === "[::1]" ||
    (isIP(u.hostname) === 4 && u.hostname.startsWith("127."));
  return { url: u, loopback };
}

console.log(`校验目录: ${PACKAGES}\n`);

// ---- plugin.json ----
console.log("1. .codebuddy-plugin/plugin.json");
const pluginPath = join(PACKAGES, ".codebuddy-plugin/plugin.json");
check(existsSync(pluginPath), "文件存在");
const plugin = JSON.parse(readFileSync(pluginPath, "utf8"));
check(
  plugin.$schema === "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
  "$schema 正确",
);
check(typeof plugin.name === "string" && /^[a-zA-Z0-9][a-zA-Z0-9._-]*$/.test(plugin.name), `name 合法 (${plugin.name})`);
check(!plugin.name.includes("..") && !plugin.name.includes("--"), "name 不含 .. 或 --");
check(typeof plugin.version === "string", `version 为字符串 (${plugin.version})`);
check(
  !Object.keys(plugin).some((k) => !PLUGIN_FIELDS.has(k)),
  `顶层字段均为标准字段 (多余: ${Object.keys(plugin).filter((k) => !PLUGIN_FIELDS.has(k)).join(",") || "无"})`,
);
check(
  Array.isArray(plugin.keywords) && plugin.keywords.every((k) => typeof k === "string"),
  "keywords 为字符串数组",
);
const ext = plugin.extensions?.["ai.workbuddy"];
check(isPluginRecord(ext), "extensions.ai.workbuddy 存在");
check(
  ext?.schemaVersion === undefined || ext.schemaVersion === "1.0.0",
  "extensions.ai.workbuddy.schemaVersion 合法",
);
for (const key of ["name", "name_en", "description", "description_zh", "description_en"]) {
  check(typeof ext?.display?.[key] === "string" && ext.display[key].length > 0, `display.${key} 非空`);
}
check(Array.isArray(ext?.display?.examples_zh) && ext.display.examples_zh.length > 0, "display.examples_zh 非空");
check(Array.isArray(ext?.display?.examples_en) && ext.display.examples_en.length > 0, "display.examples_en 非空");
check(typeof ext?.policy?.minWorkbuddyVersion === "string", "policy.minWorkbuddyVersion 存在");

// ---- mcp.json ----
console.log("\n2. mcp.json");
const mcpPath = join(PACKAGES, "mcp.json");
check(existsSync(mcpPath), "文件存在");
const mcp = JSON.parse(readFileSync(mcpPath, "utf8"));
check(
  mcp.$schema === "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json",
  "$schema 正确",
);
check(isPluginRecord(mcp.mcpServers), "mcpServers 为对象");
check(
  !Object.keys(mcp).some((k) => k !== "$schema" && k !== "mcpServers"),
  "顶层无多余字段",
);

for (const [name, srv] of Object.entries(mcp.mcpServers ?? {})) {
  console.log(`  — server: ${name}`);
  check(isPluginRecord(srv), "server 是对象");
  check(srv.type === "streamable-http" || srv.type === "sse", `type 合法 (${srv.type})`);
  const extra = Object.keys(srv).filter((k) => !REMOTE_FIELDS.has(k));
  check(extra.length === 0, `无未知 remote 字段 (多余: ${extra.join(",") || "无"})`);
  check(typeof srv.url === "string", "url 为字符串");
  let parsed;
  try {
    parsed = parseAgentPluginRemoteUrl(srv.url);
    check(true, `remote URL 合法 (${parsed.url.origin})`);
  } catch (e) {
    check(false, `remote URL 非法: ${e.message}`);
    continue;
  }
  // WB 的规则：非 loopback 的 HTTP 声明会被拒绝，必须走 HTTPS。
  check(
    parsed.url.protocol === "https:" || parsed.loopback,
    parsed.url.protocol === "https:"
      ? "使用 HTTPS"
      : `HTTP 但为 loopback（${parsed.url.hostname}），符合 WB 的例外规则`,
  );
  if (srv.headers) {
    const names = Object.keys(srv.headers);
    check(
      names.every((n) => HEADER_NAME_RE.test(n)) &&
        new Set(names.map((n) => n.toLowerCase())).size === names.length,
      "headers 名称合法且不重复",
    );
  }
}

// ---- skills ----
console.log("\n3. skills/greet/SKILL.md");
const skillPath = join(PACKAGES, "skills/greet/SKILL.md");
check(existsSync(skillPath), "文件存在");
const skill = readFileSync(skillPath, "utf8");
check(/^---\n/.test(skill), "含 frontmatter");
check(/^name:\s*\S+/m.test(skill), "frontmatter 有 name");
check(/^description:\s*\S+/m.test(skill), "frontmatter 有 description");

// ---- icon ----
console.log("\n4. ai.workbuddy/assets/icon.svg");
const iconPath = join(PACKAGES, "ai.workbuddy/assets/icon.svg");
check(existsSync(iconPath), "文件存在");
check(readFileSync(iconPath, "utf8").includes("<svg"), "是合法 SVG");

console.log();
if (problems.length) {
  console.log(`✗ ${problems.length} 项不合规`);
  process.exit(1);
}
console.log("✓ 连接器提交包完全符合 WorkBuddy 插件规范");
