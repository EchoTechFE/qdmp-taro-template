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

登录示例要求先在项目根目录的全局配置文件 `qdmp-config.json` 中填写真实 `appId` 和 `appSecret`。`variables` 保存服务端 JSON 变量；需要 MongoDB 时同时填写 `mongodb.uri` 与等于 AppID 的 `mongodb.database`，均为空表示不启用数据库。秘密不能放进 `frontend/`、公开配置或日志。

```bash
# 项目根目录：从主配置准备公开前端元数据，然后编译
qdmp build

# 检查并打包正式后端；从根主配置读取服务端字段
qdmp backend package --json
```

无需手工执行 `qdmp config sync` 才能构建或发布。该命令仅供需要预先导出公开前端配置的旧工具使用。若直接在 `frontend/` 执行原生 Taro 命令，应先导出公开配置；使用 `qdmp build` 时 CLI 自动准备。

## 后端行为

后端监听 `8080`，暴露 `/healthz`（进程）与 `/readyz`（已启用的 MongoDB）。登录经自身正式域名 `https://<AppID>.qiandaomp.com/auth/v1/token`，前端仅发送 `Taro.login()` 的 code；Go 使用根目录 `qdmp-config.json` 中配置的 `appId`、`appSecret` 请求千岛 OpenAPI；本地运行直接读取该文件，正式发布由 CLI 从同一文件读取并注入私有构建副本。刷新 Token 沿用前端直连平台 `/auth/v1/refresh` 的原实现。启动页保留原有自动登录行为；未配置 AppID 时会在发起后端请求前报错。登录示例需要先部署后端。成功换票须包含完整 Token、有效过期时间与 `data.openId`；前端保存并隔离当前会话，不从其他接口补齐身份。

启用 MongoDB 时，所有副本共享同一数据库，禁止依赖本地文件保存业务状态。服务启动、就绪检查和登录代理的错误不会回显数据库连接串或应用凭据。

详见 [前端说明](frontend/README.md)、[后端说明](backend/README.md)。

## 发布

发布顺序、变更审查、全栈与历史纯前端项目的范围判断，以及上线验收规范由 `qdmp-skill` 维护；`qdmp-cli` 执行打包、上传与部署。全栈项目先完成正式后端发布和验收，再构建上传前端；前端上传不会替代后端发布。后端包必须由 `qdmp backend package` 生成，不直接 zip 源码。

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
