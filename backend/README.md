# 小程序正式后端

本模板仅包含正式环境的 Go HTTP 服务和可选 MongoDB，不依赖浏览器、开发环境后端或本地文件持久化。`backend-template.json` 声明模板自身的配置格式、运行时和构建入口契约。

要求 Go 1.24 或更新版本。模板已直接提供项目根目录主配置 `qdmp-config.json`，克隆后即可填写。`qdmp create` 保留文件内容并设置权限为 `0600`，新项目中的实际配置按忽略规则保护；模板仓库仅提交空值。登录示例要求在项目根目录的全局配置文件 `qdmp-config.json` 中填写真实 `appId`、`appSecret`；发布也从此文件读取凭据：

已有后端但缺主配置时，执行 `qdmp config init --json` 补齐配置，不会覆盖 `backend/`。

历史纯前端项目添加服务端时，在原项目根目录执行 `qdmp backend init --json`。根目录或 `frontend/qdmp.json` 中的 AppID、开发 AppID、loader、name、latest 会首次继承到主配置，前端源码与目录保留；两处配置冲突时停止。已有主配置始终优先，前端配置不会反向覆盖它。

```json
{
  "appId": "替换为真实AppID",
  "appSecret": "",
  "runtime": "go",
  "environment": "production",
  "mongodb": { "uri": "", "database": "" },
  "variables": {}
}
```

`mongodb` 缺失、为 `null` 或 URI/database 均空时未启用；需要持久化时同时填写平台提供的实际连接串和等于 AppID 的 database，只填一项会报错。所有副本使用同一数据库；数据库连接或 primary ping 失败会阻止启动，不回退到本地文件。业务集合和索引应随实际数据模型维护。

`variables` 是后端自定义 JSON 对象，可包含嵌套值，读取后保留到正式包和 Go 运行配置。前端仅接收 AppID、loader 等公开配置，AppSecret、MongoDB 和自定义变量不向前端复制。变量或凭据变更后须重新打包。

在 `backend/` 执行：

```bash
go test ./...
go build -o /tmp/qdmp-backend main.go
go run main.go
```

本地启动从 `../qdmp-config.json` 读取配置，可通过 `QDMP_CONFIG` 指定配置路径。端口固定 8080，入口层负责 HTTPS。`GET /healthz` 检查进程，`GET /readyz` 检查已启用的 MongoDB；SIGTERM 最多等待五秒排空 HTTP 请求。

生产打包使用 `qdmp backend package`，不要直接 zip 本目录。平台只编译 `main.go`。打包器在私有临时副本注入正式配置并验证同一构建入口；源码中的配置标记必须保持为空。秘密仅能存在已忽略的本地配置、私有发布包及受控部署源码/镜像，禁止提交或输出到日志。发布包的权限、摘要与内容由 CLI 校验。

打包直接读取主配置；`qdmp config sync --json` 仅作为可选的公开前端配置导出，不是打包前置。

发布完成须同时核对 build/deploy Job 成功、全部新副本 Ready、部署版本和两个健康端点；镜像标签变化不能单独证明服务已完成切换。

登录代理使用固定的千岛 OpenAPI `/auth/v1/token` 上游，客户端仅可提交 code；应用凭据统一配置在项目根目录的 `qdmp-config.json`。本地后端直接读取该文件，正式发布时 CLI 从该文件读取并注入私有构建副本。缺少 AppSecret 时登录入口返回 503，健康检查仍可用。Token 刷新沿用原前端直接调用平台 `/auth/v1/refresh`，后端不增加刷新代理。禁止将客户端传来的 AppID、AppSecret 或任意上游 URL 透传到平台。

登录换票的成功响应必须包含非空 accessToken、refreshToken、openId 及有效 expiresAt；缺失时返回脱敏错误，不重放登录 code，也不通过其他接口补齐身份。OpenID 来自平台 AUTHORIZATION_CODE 响应；前端只在完整校验成功后保存当前用户会话。
