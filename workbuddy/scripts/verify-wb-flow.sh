#!/usr/bin/env bash
# 按 WorkBuddy 客户端的真实流程验证 MCP OAuth。
#
# 流程严格对齐 WB 内置的 MCP OAuth 实现（官方 MCP TypeScript SDK）：
#   1. POST /mcp 得到 401，从 WWW-Authenticate 取 resource_metadata
#   2. GET  /.well-known/oauth-protected-resource 取 resource 与 authorization_servers
#   3. GET  /.well-known/oauth-authorization-server 取各端点
#   4. POST /oauth/register 动态注册客户端（带 redirect_uris）
#   5. GET  /oauth/authorize 打开授权页（这里用自动登录代替人工点击）
#   6. POST /oauth/token 用 code + code_verifier 换令牌
#   7. POST /mcp 带 Bearer 令牌调用工具
#   8. POST /oauth/token 刷新令牌
#   9. 安全边界检查：授权码复用、不带 client_id、无令牌访问
#  10. POST /oauth/revoke 撤销令牌并确认其失效
set -euo pipefail

# 本地回环地址不走代理，否则 127.0.0.1 会被转发到代理服务器。
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY all_proxy

BASE="${BASE:-http://127.0.0.1:8080}"
CLIENT_NAME="WorkBuddy (wb-connector)"

REDIR="http://127.0.0.1:57099/mcp/oauth/callback"
VERIFIER="wb-connector-test-verifier-0123456789abcdef"
CHALLENGE="$(printf '%s' "$VERIFIER" | openssl dgst -binary -sha256 | openssl base64 -A | tr '+/' '-_' | tr -d '=')"
# 受保护资源标识，对应 RFC 8707 的资源指示器。
RESOURCE="$BASE/mcp"

step() { printf '\n\033[1;36m▶ %s\033[0m\n' "$1"; }
ok()   { printf '  \033[0;32m✓\033[0m %s\n' "$1"; }
fail() { printf '  \033[0;31m✗\033[0m %s\n' "$1"; exit 1; }

step "1. POST /mcp 无令牌 —— 期望 401 + WWW-Authenticate"
CHALLENGE_HDR="$(curl -sS -D - -o /dev/null -X POST "$BASE/mcp" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}' \
  | tr -d '\r')"
echo "$CHALLENGE_HDR" | grep -qi '^HTTP/1.1 401' || fail "期望 401，实际：$(echo "$CHALLENGE_HDR" | head -1)"
echo "$CHALLENGE_HDR" | grep -qi 'resource_metadata=' || fail "401 缺少 resource_metadata 挑战头"
RESOURCE_METADATA_URL="$(echo "$CHALLENGE_HDR" | grep -i 'www-authenticate:' | sed -n 's/.*resource_metadata="\([^"]*\)".*/\1/p')"
ok "401 挑战正确"
ok "resource_metadata = $RESOURCE_METADATA_URL"

step "2. GET 受保护资源元数据 (RFC 9728)"
PRM="$(curl -sS "$RESOURCE_METADATA_URL")"
echo "  $PRM"
echo "$PRM" | grep -q '"resource"' || fail "缺少 resource 字段"
echo "$PRM" | grep -q '"authorization_servers"' || fail "缺少 authorization_servers 字段"
AUTHZ_SERVER="$(echo "$PRM" | python3 -c 'import sys,json;print(json.load(sys.stdin)["authorization_servers"][0])')"
ok "authorization_servers[0] = $AUTHZ_SERVER"

step "3. GET 授权服务器元数据"
# 客户端按 buildDiscoveryUrls 依次尝试，这里用 WB 实际命中的那个路径。
AS_META="$(curl -sS "$AUTHZ_SERVER/.well-known/oauth-authorization-server")"
echo "  $AS_META"
for f in issuer authorization_endpoint token_endpoint registration_endpoint; do
  echo "$AS_META" | python3 -c "import sys,json;d=json.load(sys.stdin);assert d.get('$f'),'缺少 $f'" \
    || fail "授权服务器元数据缺少 $f"
done
echo "$AS_META" | python3 -c "
import sys,json
d=json.load(sys.stdin)
assert 'code' in d.get('response_types_supported',[]), 'response_types_supported 必须含 code'
assert 'S256' in d.get('code_challenge_methods_supported',[]), 'code_challenge_methods_supported 必须含 S256'
" || fail "授权服务器元数据字段不满足 WB 客户端校验"
ok "授权服务器元数据完整且满足 WB 客户端 schema"

step "4. POST /oauth/register 动态注册"
REG="$(curl -sS -X POST "$AUTHZ_SERVER/oauth/register" \
  -H 'Content-Type: application/json' \
  -d "$(python3 - "$CLIENT_NAME" "$REDIR" <<'PY'
import json,sys
print(json.dumps({
  "client_name": sys.argv[1],
  "application_type": "native",
  "redirect_uris": [sys.argv[2]],
  "grant_types": ["authorization_code","refresh_token"],
  "response_types": ["code"],
  "token_endpoint_auth_method": "none",
  "logo_uri": None,
  "tos_uri": None,
}))
PY
)")"
echo "  $REG"
CLIENT_ID="$(echo "$REG" | python3 -c 'import sys,json;print(json.load(sys.stdin)["client_id"])')"
echo "$REG" | python3 -c "
import sys,json
d=json.load(sys.stdin)
assert d.get('redirect_uris'), '必须回显 redirect_uris'
assert d.get('client_id'), '必须返回 client_id'
" || fail "注册响应不合规"
ok "client_id = $CLIENT_ID"

step "5. GET /oauth/authorize 拿授权码"
AUTHZ="$(curl -sS -D - -o /dev/null -G "$AUTHZ_SERVER/oauth/authorize" \
  --data-urlencode "client_id=$CLIENT_ID" \
  --data-urlencode "redirect_uri=$REDIR" \
  --data-urlencode "state=wb-state-abc" \
  --data-urlencode "code_challenge=$CHALLENGE" \
  --data-urlencode "code_challenge_method=S256" \
  --data-urlencode "response_type=code" \
  --data-urlencode "resource=$RESOURCE" | tr -d '\r')"
echo "$AUTHZ" | grep -qi '^HTTP/1.1 200' || fail "期望授权页 200，实际：$(echo "$AUTHZ" | head -1)"
ok "授权页正常返回"

# 用登录表单提交换取授权码，代替人工在浏览器里点登录。
# 登录成功后服务端返回的是授权成功页（HTML），回调地址在页面里，
# 而不是 302 的 Location 头。
LOGIN_BODY="$(curl -sS -X POST "$AUTHZ_SERVER/oauth/login" \
  --data-urlencode "client_id=$CLIENT_ID" \
  --data-urlencode "redirect_uri=$REDIR" \
  --data-urlencode "state=wb-state-abc" \
  --data-urlencode "code_challenge=$CHALLENGE" \
  --data-urlencode "code_challenge_method=S256" \
  --data-urlencode "username=demo" \
  --data-urlencode "password=demo123")"
CODE="$(printf '%s' "$LOGIN_BODY" | grep -oE 'href="[^"]+"' | head -1 | sed -e 's/^href="//' -e 's/"$//' \
  | sed -n 's/.*[?&]code=\([^&]*\).*/\1/p')"
[ -n "$CODE" ] || fail "登录后未拿到授权码"
ok "拿到授权码 ${CODE:0:12}..."

step "6. POST /oauth/token 换令牌"
TOKEN="$(curl -sS -X POST "$AUTHZ_SERVER/oauth/token" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode "grant_type=authorization_code" \
  --data-urlencode "code=$CODE" \
  --data-urlencode "redirect_uri=$REDIR" \
  --data-urlencode "client_id=$CLIENT_ID" \
  --data-urlencode "code_verifier=$VERIFIER" \
  --data-urlencode "resource=$RESOURCE")"
echo "  $TOKEN"
ACCESS_TOKEN="$(echo "$TOKEN" | python3 -c 'import sys,json;print(json.load(sys.stdin)["access_token"])')"
REFRESH_TOKEN="$(echo "$TOKEN" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("refresh_token",""))')"
echo "$TOKEN" | python3 -c "import sys,json;d=json.load(sys.stdin);assert d.get('token_type')=='Bearer','token_type 必须是 Bearer'" || fail "token_type 不合规"
ok "access_token = ${ACCESS_TOKEN:0:12}..."

step "7. POST /mcp 带令牌调用工具"
RESULT="$(curl -sS -X POST "$BASE/mcp" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'MCP-Protocol-Version: 2025-06-18' \
  -d '{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"wb-connector","version":"1"}}}')"
echo "$RESULT" | head -c 400
echo
echo "$RESULT" | grep -q 'serverInfo' || fail "带令牌调用失败，未返回 serverInfo"
ok "带 Bearer 令牌调用成功"

step "8. 刷新令牌"
REFRESHED="$(curl -sS -X POST "$AUTHZ_SERVER/oauth/token" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode "grant_type=refresh_token" \
  --data-urlencode "refresh_token=$REFRESH_TOKEN" \
  --data-urlencode "client_id=$CLIENT_ID" \
  --data-urlencode "resource=$RESOURCE")"
echo "$REFRESHED" | python3 -c "import sys,json;d=json.load(sys.stdin);assert d.get('access_token'),'刷新失败'" || fail "刷新令牌失败"
ok "刷新成功"

step "9. 安全边界检查"
# 授权码一次性：同一个 code 再换一次必须被拒绝。
REUSE="$(curl -sS -X POST "$AUTHZ_SERVER/oauth/token" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode "grant_type=authorization_code" \
  --data-urlencode "code=$CODE" \
  --data-urlencode "redirect_uri=$REDIR" \
  --data-urlencode "client_id=$CLIENT_ID" \
  --data-urlencode "code_verifier=$VERIFIER")"
echo "$REUSE" | grep -q 'invalid' || fail "授权码复用未被拒绝: $REUSE"
ok "授权码复用已拒绝"

# 换令牌必须带 client_id：省略它等于放弃客户端身份校验。
NO_CLIENT="$(curl -sS -X POST "$AUTHZ_SERVER/oauth/token" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode "grant_type=authorization_code" \
  --data-urlencode "code=$CODE" \
  --data-urlencode "redirect_uri=$REDIR" \
  --data-urlencode "code_verifier=$VERIFIER")"
echo "$NO_CLIENT" | grep -q 'invalid_client' || fail "不带 client_id 未被拒绝: $NO_CLIENT"
ok "不带 client_id 换令牌已拒绝"

# 无令牌访问 /mcp 必须 401。
NO_TOKEN="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/mcp" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/list"}')"
[ "$NO_TOKEN" = "401" ] || fail "无令牌访问未被拒绝，实际 $NO_TOKEN"
ok "无令牌访问已拒绝"

step "10. 撤销令牌（RFC 7009）"
REVOKE="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$AUTHZ_SERVER/oauth/revoke" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode "token=$ACCESS_TOKEN" \
  --data-urlencode "token_type_hint=access_token" \
  --data-urlencode "client_id=$CLIENT_ID")"
[ "$REVOKE" = "200" ] || fail "撤销令牌失败，状态码 $REVOKE"
ok "撤销成功"

# 撤销后访问令牌立即失效。
AFTER_REVOKE="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/mcp" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/list"}')"
[ "$AFTER_REVOKE" = "401" ] || fail "撤销后令牌仍然可用，实际 $AFTER_REVOKE"
ok "撤销后令牌已失效"

printf '\n\033[0;32m全部通过：WorkBuddy 客户端流程可完整跑通\033[0m\n'
