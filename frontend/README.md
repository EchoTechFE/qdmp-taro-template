# 千岛小程序开发模板（前端）

基于 [Taro 4](https://docs.taro.zone/) + Vue 3 的千岛小程序开发模板，为千岛开发者提供开箱即用的项目脚手架，内置千岛 OpenAPI 鉴权流程与接口调用示例。

## 功能特性

- 千岛 OpenAPI 鉴权：`Taro.login` 获取 code → 换取 access-token，自动刷新
- 请求层封装：基于 `Taro.request`，自动注入 token、401 重试
- OpenAPI 调用示例：岛详情（SPU Detail）接口调用与数据展示
- 多端支持：千岛小程序、H5

## 项目结构

```
├── config/                  # Taro 构建配置
│   ├── index.js             # 基础配置
│   ├── dev.js               # 开发环境
│   └── prod.js              # 生产环境
├── src/
│   ├── apis/
│   │   ├── auth.js          # 鉴权：登录、token 存取与刷新
│   │   ├── request.js       # 请求封装：token 注入、401 自动刷新重试
│   │   └── spu.js           # SPU 相关接口（搜索、详情）
│   ├── pages/
│   │   └── index/           # 首页：欢迎页 + OpenAPI 调用示例
│   ├── app.js               # 应用入口
│   ├── app.config.js        # 应用配置（页面路由、导航栏）
│   └── app.scss             # 全局样式
└── project.config.json      # 小程序项目配置
```

## 快速开始

### 环境要求

- Node.js 22.12+（22.x）、24.11+（24.x）或 26+，与完整构建所用 qdmp-cli 的要求一致
- pnpm >= 10
### 安装依赖

```bash
pnpm install --frozen-lockfile
```

### 配置应用信息

在项目根目录的全局配置文件 `qdmp-config.json` 中填写真实 `appId`、`appSecret`，保留其他配置字段。模板已直接包含该文件，克隆后即可填写，无需额外生成。AppSecret 由后端读取；前端只使用构建时准备的公开 AppID。AppSecret 变更后需重新打包部署后端。

常规构建在项目根目录执行 `qdmp build`，CLI 会自动准备 `frontend/qdmp.json`。如果直接在前端目录运行原生 Taro 命令，先在根目录执行 `qdmp config sync --json` 导出公开配置，再进入 `frontend/`。详见[全栈配置与发布说明](../README.md)。

登录示例需先部署自身后端的 `/auth/v1/token`；刷新沿用前端直连平台 `/auth/v1/refresh` 的原实现。启动页保持原有自动登录行为，未配置 AppID 时会在请求前报错。

### 启动开发

```bash
# 千岛小程序（需配合千岛开发者工具）
pnpm dev

# H5
pnpm dev:h5
```

### 构建发布

```bash
# 千岛小程序
pnpm build

# H5
pnpm build:h5

# 构建并上传
pnpm upload
```

## 鉴权流程

```
Taro.login() → 获取 code
       ↓
POST 自身后端 /auth/v1/token  { code }
       ↓
后端使用根 qdmp-config.json 的 appId / appSecret 请求平台
       ↓
返回 accessToken / refreshToken / expiresAt / openId → 校验后存入 Storage
       ↓
后续请求自动注入 access-token 头，到期前主动刷新
```

- 登录成功须同时通过 HTTP、业务 code 和完整会话校验；OpenID 只读取换票响应的 `data.openId`，不从 `user.me` 补齐
- `getOpenId()` 读取当前用户身份；刷新保留原 refreshToken 与 OpenID，退出清理完整会话，旧异步结果不会覆盖新会话
- token 到期前 5 分钟自动刷新
- 并发请求共享同一个刷新 Promise，避免重复刷新
- 请求层捕获 401 异常时，按原逻辑刷新 token 并重试一次

## API 封装

### 创建请求实例

```js
import { createRequest } from './apis/request';

// 指向千岛 OpenAPI（已内置）
import { api } from './apis/request';

// 指向自定义后端服务
import { backendApi } from './apis/request';

// 或创建新实例
const myApi = createRequest('https://your-api-base.com');
```

### 使用方式

```js
// GET 请求 — 参数拼在 URL 上（推荐，避免真机兼容问题）
const data = await api.get('/spu/v1/detail?id=123');

// POST 请求
const data = await api.post('/some/path', { key: 'value' });
```

> 注意：小程序真机环境中，GET 请求的 `data` 参数可能不会自动拼接到 URL，建议手动拼接 query string。

`spu.js` 保留原有 `searchSpu` 和 `getSpuDetail` 导出。本次后端基础模板未新增 `/spu/v1/search` 业务实现；使用该原有搜索示例前需按实际业务契约接入对应后端。

## 技术栈

| 技术 | 版本 |
| --- | --- |
| Taro | 4.2.0 |
| Vue | 3.x |
| 构建工具 | Vite |
| CSS 预处理 | Sass |
| 代码规范 | ESLint + Stylelint + Commitlint |

## 相关资源

- [Taro 官方文档](https://docs.taro.zone/)
- [千岛开放平台](https://open.qiandao.com/)
