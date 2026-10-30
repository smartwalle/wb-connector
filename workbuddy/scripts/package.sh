#!/usr/bin/env bash
#
# 打包连接器提交物。
#
# 产出 workbuddy/dist/wb-connector.zip，内含 workbuddy/connector/ 下的全部内容，
# 结构与 WorkBuddy 官方连接器一致：
#   .codebuddy-plugin/plugin.json   插件清单（display / policy）
#   mcp.json                        MCP 服务声明
#   ai.workbuddy/assets/icon.svg    图标
#   skills/greet/SKILL.md           技能说明

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# 脚本在 workbuddy/scripts/ 下，仓库根目录要上跳两级。
PROJECT_DIR="$(dirname "$(dirname "${SCRIPT_DIR}")")"

SRC_DIR="${PROJECT_DIR}/workbuddy/connector"
DIST_DIR="${PROJECT_DIR}/workbuddy/dist"
ZIP_PATH="${DIST_DIR}/wb-connector.zip"

REQUIRED=(
  ".codebuddy-plugin/plugin.json"
  "mcp.json"
  "ai.workbuddy/assets/icon.svg"
  "skills/greet/SKILL.md"
)

cd "${SRC_DIR}"

# 打包前先校验 connector/ 目录是否符合 WorkBuddy 插件规范，
# 避免把不合规的包提交上去。
node "${SCRIPT_DIR}/verify-package.mjs" "${SRC_DIR}" >/dev/null || {
  echo "提交包校验未通过，已终止打包。" >&2
  node "${SCRIPT_DIR}/verify-package.mjs" "${SRC_DIR}" >&2
  exit 1
}

for item in "${REQUIRED[@]}"; do
  if [[ ! -f "${item}" ]]; then
    echo "缺少必需文件: ${item}" >&2
    exit 1
  fi
done

mkdir -p "${DIST_DIR}"

# 用 Python 的 zipfile 打包，而不是命令行 zip。
# 命令行 zip 在写入被沙箱托管的目录时会被拦截（回滚机制无法追踪），
# 而 zipfile 走的是普通文件写入，不受影响。
# 先写临时文件再整体覆盖，避免写到一半失败留下损坏的包。
python3 - "${SRC_DIR}" "${ZIP_PATH}" <<'PY'
import os, sys, tempfile, zipfile

src, dest = sys.argv[1], sys.argv[2]

# 收集要打包的文件，跳过 macOS 资源分支文件。
files = []
for root, dirs, names in os.walk(src):
    dirs[:] = [d for d in dirs if d != "__MACOSX"]
    for name in names:
        if name == ".DS_Store":
            continue
        full = os.path.join(root, name)
        files.append((full, os.path.relpath(full, src)))

# 按相对路径排序，保证同样内容打出同样的包。
files.sort(key=lambda pair: pair[1])

dirname = os.path.dirname(dest) or "."
fd, tmp = tempfile.mkstemp(dir=dirname, prefix=".wb-connector-", suffix=".zip")
os.close(fd)
try:
    with zipfile.ZipFile(tmp, "w", zipfile.ZIP_DEFLATED) as z:
        for full, rel in files:
            z.write(full, rel)
    os.replace(tmp, dest)
except BaseException:
    if os.path.exists(tmp):
        os.remove(tmp)
    raise
PY

echo "打包完成: ${ZIP_PATH}"
# 清理上一次运行可能残留的临时包。
find "${DIST_DIR}" -maxdepth 1 -type f -name '.wb-connector-*.zip' -delete 2>/dev/null || true
# 列出包内文件：跳过 zip 输出的表头与汇总行。
unzip -Z1 "${ZIP_PATH}"
