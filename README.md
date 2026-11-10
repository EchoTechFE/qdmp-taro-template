# 千岛小程序全栈模板

此仓库是 `qdmp create <项目名>`（或 `-t default`）唯一的默认模板源。一次克隆即可获得 Taro / Vue 前端与正式环境 Go 后端，不再由 CLI 内置后端源码拼装。后端仅使用正式环境，不含开发环境部署流程或浏览器依赖。

```text
frontend/                    Taro 4 / Vue 3 小程序与原有锁文件
backend/                     Go HTTP 服务、MongoDB 与登录代理
qdmp-config.json             随模板提供的空主配置，填写本地实际信息
```

## 创建与配置

```bash
qdmp create my-miniapp -t default
cd my-miniapp
cd frontend
pnpm install --frozen-lockfile
cd ..
```

模板直接包含根目录 `qdmp-config.json`，克隆后即可填写，无需额外生成或复制配置文件。`qdmp create` 校验完整模板后保留该文件内容，设置权限为 `0600` 并加入忽略规则；原有下载流程会移除模板的 `.git`，新项目中的实际配置不会沿用模板的 Git 跟踪记录。模板仓库仅提交空配置。

应用身份与服务端凭据统一填写在根目录 `qdmp-config.json` 的 `appId`、`appSecret` 字段。`variables` 保存服务端 JSON 变量；需要 MongoDB 时同时填写 `mongodb.uri` 与等于 AppID 的 `mongodb.database`，均为空表示不启用数据库。秘密不能放进 `frontend/`、公开配置或日志。

```bash
# 项目根目录：从主配置准备公开前端元数据，然后编译
qdmp build

# 检查并打包正式后端；从根主配置读取服务端字段
qdmp backend package --json
```

无需手工执行 `qdmp config sync` 才能构建或发布。该命令仅供需要预先导出公开前端配置的旧工具使用。若直接在 `frontend/` 执行原生 Taro 命令，应先导出公开配置；使用 `qdmp build` 时 CLI 自动准备。

旧项目应先执行 `qdmp config migrate --check --json` 和 `qdmp config migrate --json`，统一根主配置、`frontend/` 和真实 EMP 前端，保留已有后端语言与业务。后端就绪状态单独记录，不阻断仅前端操作；具体迁移规则见[后端说明](backend/README.md)。

## 后端行为

后端监听 `8080`，暴露 `/healthz`（进程）与 `/readyz`（已启用的 MongoDB）。认证接口的输入输出见[后端说明](backend/README.md)。登录换票、Token / OpenID 和会话处理规范统一维护在 [qdmp-skill 开发指南](https://github.com/EchoTechFE/qdmp-skill/blob/main/skills/qdmp-skill/knowledge/development-guide.md#用户登录换票与-openid)，模板不另行维护操作流程。

启用 MongoDB 时，所有副本共享同一数据库，禁止依赖本地文件保存业务状态。服务启动、就绪检查和登录代理的错误不会回显数据库连接串或应用凭据。

详见 [前端说明](frontend/README.md)、[后端说明](backend/README.md)。

## 发布

发布范围、可选 diff 审查及上线验收规范由 `qdmp-skill` 维护；`qdmp-cli` 执行构建、上传与部署。全栈项目先完成正式后端发布和验收，再构建上传前端；明确仅前端或仅后端时只执行对应步骤。

已明确部署时，执行 `qdmp backend deploy --description "<本次说明>" --json`。CLI 内部构建当前最新代码、校验归档与配置摘要、自动选择新补丁版本并直接发布，无需先单独打包或确认摘要。`--review` 为可选 diff 审查；仅独立构建检查时使用 `qdmp backend package --json`，不要直接 zip 源码。

修改此仓库后，先合并模板变更，再发布依赖新布局的 CLI。仍为旧前端布局的模板会被新 CLI 明确拒绝，避免生成缺失后端的项目。

## 验证

```bash
node --test frontend/test/auth.test.mjs
cd backend
go test ./...
go build -o /tmp/qdmp-backend main.go
cd ..
qdmp build
```

最低 Go 版本为 `1.24`；完整构建使用 Node.js 22.12+（22.x）、24.11+（24.x）或 26+，pnpm 按前端包的 packageManager 声明使用 10.14.0。后端模板契约固定单入口 `main.go`，源配置标记保持为空，打包器仅在私有临时副本中注入实际配置。
