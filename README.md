# wb-connector

Go 项目模板。分层架构：cmd → app → transport → service → domain，foundation 提供平台能力。

同时内置 MCP（Model Context Protocol）协议支持：MCP 端点与普通 HTTP 接口共用同一个
server、同一个端口，MCP 只是 HTTP 路由上的一条路径。

## 目录结构

```
wb-connector/
├── cmd/
│   ├── main.go                    # 进程入口：加载配置 → Prepare → Run
│   ├── config.yaml                # 实际配置
│   └── config.example.yaml        # 配置模板
├── internal/
│   ├── app/
│   │   ├── application.go         # 依赖组装：创建实现、注入、交给 bootstrap
│   │   └── environment.go         # 资源创建与释放
│   ├── config/
│   │   ├── config.go              # 组合各模块的 Config
│   │   └── load.go                # YAML 严格解码 + 校验
│   ├── domain/
│   │   ├── authdomain/             # 认证模块：实体、业务契约、上下文工具
│   │   └── greetdomain/            # 问候模块：实体、业务契约
│   ├── foundation/                # 平台层：与业务无关的基础能力
│   │   ├── errors/                # 统一业务错误类型与错误码
│   │   ├── logging/               # slog 初始化
│   │   └── metadata/              # 请求 id 等上下文元数据
│   ├── service/                   # 业务用例实现
│   └── transport/
│       ├── http/                  # HTTP 传输层：唯一监听点
│       │   ├── config.go
│       │   ├── server.go          # rx engine + http.Server
│       │   ├── middleware.go
│       │   ├── middleware_auth.go # Bearer 令牌校验中间件
│       │   ├── handler/           # 协议适配
│       │   │   ├── health.go
│       │   │   └── oauth.go       # OAuth 授权端点
│       │   └── jsoncodec/         # 统一响应格式与错误映射
│       └── mcp/                   # MCP 协议层
│           ├── server.go          # MCP Server 封装与工具注册
│           └── tools.go           # 工具处理器
├── workbuddy/                    # 交付成 WorkBuddy 连接器所需的一切
│   ├── connector/                #   提交至连接器市场的内容
│   │   ├── .codebuddy-plugin/plugin.json  # 插件清单：display / policy
│   │   ├── mcp.json              #   MCP 连接配置
│   │   ├── ai.workbuddy/assets/icon.svg  #   市场图标
│   │   └── skills/greet/SKILL.md #   技能说明
│   ├── scripts/
│   │   ├── package.sh            #   打包提交物（打包前自动校验规范）
│   │   ├── verify-wb-flow.sh     #   按 WB 客户端流程验证 OAuth
│   │   └── verify-package.mjs    #   校验提交包是否符合 WB 插件规范
│   └── dist/                     #   打包产物（gitignore）
└── go.mod
```

> `workbuddy/` 是平台维度的目录：`connector/` 里的包格式（`extensions.ai.workbuddy`
> 扩展、`ai.workbuddy/assets/` 资源路径）与 `scripts/` 里的校验规则都依赖
> WorkBuddy 专有规范。将来若要支持其他平台，在同级并列对应目录即可。

## 分层约定

| 层 | 职责 | 依赖方向 |
|---|---|---|
| `cmd` | 进程入口，加载配置并启动 | → app |
| `app` | 依赖组装与生命周期协调 | → transport / service / repository |
| `transport` | 协议适配，把外部协议映射到业务用例 | → service（通过 domain 契约） |
| `service` | 业务用例实现 | → domain |
| `domain` | 实体、业务契约、领域错误 | 不依赖任何层 |
| `foundation` | 平台能力，与业务无关 | 不依赖业务层 |

关键约定：

- **依赖倒置**：`domain` 定义接口（`greetdomain.Service`、`authdomain.Service`），
  实现放在 `service`，由 `app` 层注入。
- **配置归模块自持**：`transport/http` 维护 `http` 段，`foundation/logging` 维护 `logging` 段，
  `config` 只负责组合与校验。
- **平台层无业务**：`foundation` 里不允许出现业务概念。

## 运行

配置在 `cmd/config.yaml`，`main()` 用相对路径读取，因此需要在 `cmd` 目录下启动：

```bash
cd cmd
go run .            # 或 go build -o ../bin/app . && ../bin/app
```

## 本地测试

### 一、对齐 WorkBuddy 客户端流程验证

`verify-wb-flow.sh` 严格按 WorkBuddy 内置的 MCP OAuth 实现（官方 SDK `@modelcontextprotocol/sdk`）的顺序执行：

```bash
./workbuddy/scripts/verify-wb-flow.sh   # 默认 http://127.0.0.1:8080
```

| 步骤 | 对应 WB 客户端行为 |
|---|---|
| 1 | `POST /mcp` 无令牌 → 401，从 `WWW-Authenticate` 取 `resource_metadata`（RFC 9728 发现入口） |
| 2 | `GET /.well-known/oauth-protected-resource` → 取 `authorization_servers[0]` |
| 3 | `GET /.well-known/oauth-authorization-server` → 校验 zod schema 要求的字段 |
| 4 | `POST /oauth/register` 动态注册，回显 `redirect_uris` |
| 5 | `GET /oauth/authorize` → 渲染登录页 |
| 6 | `POST /oauth/login` → 302 带回 `code` |
| 7 | `POST /oauth/token` → 换令牌（PKCE S256） |
| 8 | `POST /oauth/token` 刷新令牌 |
| 9 | 安全边界：授权码复用被拒绝、无令牌访问被拒绝 |

默认账号是 `demo` / `demo123`。

### 二、在 WorkBuddy 客户端里测试

客户端从 `~/.workbuddy/mcp.json`（**不带点前缀**）读取自定义 MCP 配置：

```json
{
  "mcpServers": {
    "wb-connector": {
      "type": "http",
      "url": "http://127.0.0.1:8080/mcp",
      "timeout": 30000
    }
  }
}
```

> `type` 填 `http`。客户端内部会把 `streamable-http` 归一化成 `http`，两者等价，
> 但面板里显示与保存的是 `http`，填 `http` 可以避免配置被面板改写。

然后：

1. 左侧栏「专家·技能·连接器」→「连接器」标签 → 右上角「自定义连接器」
2. 在「MCP 服务管理」里找到 `wb-connector` → 点「**信任**」
3. **再点「授权」或「连接」** ← 这一步不能省。信任只是让 server 进入候选清单；
   点连接后客户端会 `POST /mcp` 拿到 401，判定为 `unauthorized`，
   才会弹出浏览器打开授权页
4. 浏览器打开 `/oauth/authorize`，用 `demo` / `demo123` 登录，
   授权完成后回调深链（`workbuddy://...`）回到客户端
5. **新建会话**（MCP 工具清单在会话启动时注入，当前会话不会热更新）
6. 说「用 greet 工具向小明问个好」

> 没走到第 3 步的话，连接器的工具**根本不会注入**会话 ——
> 表现是 `ToolSearch` 搜 `greet` 搜不到，而不是调用时报未授权。

客户端点「连接」时的真实流程（可在 WorkBuddy 安装包中核对）：

```js
connect(id) → 若返回 state === 'unauthorized' 且 authMode === 'oauth'
            → beginAuthorization(id) → shell.openExternal(authorizeUrl)
            → 浏览器完成登录 → 深链回调 → 客户端换令牌
```

`http` 类型的 MCP 会自动被判定为 `oauth` 模式，无需在配置里额外声明。

> 同一个地址不要配置多个条目，工具名会冲突。

信任记录保存在 `~/.workbuddy/mcp-approvals.json`，键为
`sha256(<origin>)::<server 名>`。

## 认证

服务内置 MCP 授权服务器（OAuth 2.1 + PKCE）。用户在 WorkBuddy 上首次使用连接器时，
会先在浏览器完成登录，之后 WorkBuddy 自动携带令牌调用。

```yaml
http:
  # 授权服务器对外可达的地址。生产环境必须使用 HTTPS。
  oauth_issuer: "https://mcp.example.com"
  # 受保护资源的标识，通常与 MCP 端点地址一致。
  oauth_resource: "https://mcp.example.com/mcp"
```

### 端点

| 路径 | 方法 | 作用 |
|---|---|---|
| `/mcp` | POST/GET | MCP 端点，需要 Bearer 令牌 |
| `/.well-known/oauth-protected-resource` | GET | 受保护资源元数据（RFC 9728） |
| `/.well-known/oauth-authorization-server` | GET | 授权服务器元数据（RFC 8414） |
| `/oauth/register` | POST | 动态客户端注册（RFC 7591） |
| `/oauth/authorize` | GET | 授权端点，渲染登录页 |
| `/oauth/login` | POST | 登录表单提交 |
| `/oauth/token` | POST | 换取令牌、刷新令牌 |
| `/oauth/revoke` | POST | 撤销令牌（RFC 7009） |

### 遵循的规范

| 规范 | 落实情况 |
|---|---|
| RFC 6749 | 授权码流程、刷新令牌轮换、令牌端点错误码细分 |
| RFC 7636 | PKCE，强制 S256，不接受 plain |
| RFC 7591 | 动态客户端注册，回显 `redirect_uris` 并校验其合法性 |
| RFC 8252 | 回环回调地址允许端口变化，但 scheme/主机/路径/查询串须与登记值一致 |
| RFC 8414 | 授权服务器元数据 |
| RFC 8707 | 元数据里声明 `resource`，供客户端指定令牌的目标资源 |
| RFC 9728 | 受保护资源元数据发现入口 |
| RFC 7009 | 令牌撤销端点 |

> `revocation_endpoint` 目前**不在元数据里声明**：WorkBuddy 客户端要求撤销端点
> 必须是 HTTPS，而本地开发用的是 HTTP 授权服务器，声明了反而会让客户端
> 判为不安全地址。端点本身已实现，部署到 HTTPS 后在元数据里加上该字段即可启用。

### 认证流程

```
WorkBuddy 请求 /mcp（无令牌）
  → 401 + WWW-Authenticate: Bearer resource_metadata="..."
  → 发现受保护资源元数据 → 发现授权服务器元数据
  → 动态注册客户端（登记客户端自己的回调地址）
  → 打开浏览器 /oauth/authorize
  → 用户在登录页输入账号密码
  → 302 跳回客户端 redirect_uri?code=...&state=...
  → WorkBuddy 用授权码 + code_verifier 换令牌
  → 之后携带 Authorization: Bearer <token> 调用
```

> 客户端登记的回调地址由它自己决定（实测是 `workbuddy://` 深链协议），
> 因此服务端**必须支持动态注册**（`POST /oauth/register`），
> 不能只认固定的回调地址。

### 账号

示例实现的账号固定在 `internal/service/oauth.go` 的常量里：

```go
var accounts = map[string]string{
    "demo": "demo123",
}
```

生产环境应替换为真实用户体系，并去掉明文密码。

### 实现要点

- **PKCE 强制 S256**：`code_challenge_method` 不是 `S256` 时拒绝授权。
- **授权码一次性**：用过即从存储中移除，无论后续校验是否成功，避免并发复用。
- **回调地址精确匹配**：必须与客户端注册时登记的 `redirect_uris` 中某一项完全相同，
  防止授权码被用于攻击者控制的回调地址。
- **令牌校验中间件只挂 MCP 路由**：业务接口（如 `/health`）不需要认证。
- **状态保存在内存**：进程重启后授权码与令牌全部失效。

## MCP 接入

MCP 端点与业务接口共用同一个 HTTP server 和同一个端口，MCP 只是路由上的一条路径。

端点路径由 `internal/transport/mcp` 的常量 `Path` 定义（默认 `/mcp`），
不通过配置文件调整——传输方式固定为 streamable-http，没有第二种选择要配。

```go
// internal/transport/mcp/server.go
const Path = "/mcp"
```

客户端配置示例：

```json
{
  "mcpServers": {
    "wb-connector": {
      "type": "streamable-http",
      "url": "http://127.0.0.1:8080/mcp"
    }
  }
}
```

- `POST /mcp` 承载 JSON-RPC 请求与响应。
- `GET /mcp` 建立 SSE 推送流，用于接收服务端主动通知。
- 当前 `Stateless: true`（无状态模式），服务端不保存会话状态，
  每个请求独立处理，`Mcp-Session-Id` 响应头为空。

> 无状态模式下 `GET /mcp` 会返回 405——这是 MCP SDK 的行为，与本项目无关。
> 将来若要启用服务端主动推送，需把 `Stateless` 改为 `false`。

> 路由注册时路径要用 `mcp.Path` 本身，不能写成 `r.Group("/mcp")` 再挂 `"/"`——
> 分组会把路径拼成 `/mcp/`，而客户端配置的 URL 不带尾斜杠，两者不匹配会返回 404。

### 添加 MCP 工具

工具定义在 `internal/transport/mcp/tools.go`，业务规则在 `internal/service`：

```go
// internal/transport/mcp/tools.go
mcp.AddTool(server, &mcp.Tool{Name: "greet", ...}, newGreetHandler(greetService))
```

`newGreetHandler` 只做协议适配：MCP 入参 → `greetdomain.GreetRequest` → 调用用例 →
结果转回 MCP 输出。新增工具时照此办理，业务规则不要写在传输层。

## 打包提交

提交至连接器市场的内容都在 `workbuddy/connector/` 目录，打包命令：

```bash
./workbuddy/scripts/package.sh    # 产出 workbuddy/dist/wb-connector.zip
```

包内结构与 WorkBuddy 官方连接器一致：

```
.codebuddy-plugin/plugin.json
mcp.json
ai.workbuddy/assets/icon.svg
skills/greet/SKILL.md
```

提交前需把 `workbuddy/connector/mcp.json` 里的 `url` 换成实际部署地址，
并把 `cmd/config.yaml` 的 `oauth_issuer`、`oauth_resource` 一并改成该地址——
两处必须一致，否则客户端发现授权服务器时会指向错误地址。

## 技术栈

| 依赖 | 版本 | 用途 |
|---|---|---|
| [github.com/smartwalle/rx](https://github.com/smartwalle/rx) | v0.0.8 | HTTP 路由与中间件 |
| [github.com/smartwalle/bootstrap](https://github.com/smartwalle/bootstrap) | v0.0.4 | 应用生命周期管理 |
| [github.com/smartwalle/logkit](https://github.com/smartwalle/logkit) | v0.0.0-20261012130630 | 日志 |
| [github.com/modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) | v1.8.0 | MCP 协议实现（官方） |
| gopkg.in/yaml.v3 | v3.0.1 | 配置解析 |

> 账号固定写在代码里，不接数据库，因此没有引入 ORM 或数据库驱动。

## 常用命令

```bash
go build ./...              # 编译
go vet ./...                # 静态检查
gofmt -l ./cmd ./internal   # 格式检查
go test ./...               # 单元测试
```
