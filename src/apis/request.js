import Taro from "@tarojs/taro";
import { tokenNeedsRefresh, ensureRefresh, doLogin, clearTokens } from "./auth";
import { ACCESS_TOKEN_KEY, AUTH_REFRESH_PATH, OPENAPI_BASE, BACKEND_BASE } from "./config";

/**
 * 创建带 access-token 自动注入的请求实例
 * @param {string} baseURL - 接口域名
 */
export function createRequest(baseURL) {
  const request = async (options) => {
    const { url, method = "GET", data, header = {}, ...rest } = options;

    const fullUrl = url.startsWith("http") ? url : `${baseURL}${url}`;
    const isRefreshCall = url?.includes(AUTH_REFRESH_PATH);

    let accessToken = Taro.getStorageSync(ACCESS_TOKEN_KEY);

    if (accessToken && !isRefreshCall && tokenNeedsRefresh()) {
      try {
        const refreshData = await ensureRefresh();
        accessToken = refreshData.accessToken || Taro.getStorageSync(ACCESS_TOKEN_KEY);
      } catch (e) {
        console.warn("[request] proactive refresh failed:", e);
      }
    }

    const headers = {
      accept: "application/json",
      "content-type": "application/json",
      ...header,
    };

    if (accessToken) {
      headers["access-token"] = accessToken;
    }

    try {
      const res = await Taro.request({
        url: fullUrl,
        method,
        data,
        header: headers,
        ...rest,
      });

      // iOS 上 Taro.request 对非 2xx 可能不抛异常，需要主动检查
      if (res.statusCode && res.statusCode >= 400) {
        const err = new Error(`HTTP ${res.statusCode}`);
        err.statusCode = res.statusCode;
        err.data = res.data;
        throw err;
      }

      return res.data;
    } catch (err) {
      const status = err?.statusCode ?? err?.status;

      if (status === 401 && !isRefreshCall && !options._retried) {
        try {
          // 先尝试 refreshToken 刷新
          const refreshData = await ensureRefresh();
          const newToken = refreshData.accessToken || Taro.getStorageSync(ACCESS_TOKEN_KEY);
          return request({
            ...options,
            _retried: true,
            header: { ...header, "access-token": newToken },
          });
        } catch (_refreshErr) {
          // refresh 也失败了，清除旧 token，重新走静默登录
          try {
            clearTokens();
            const loginData = await doLogin();
            const newToken = loginData.accessToken || Taro.getStorageSync(ACCESS_TOKEN_KEY);
            return request({
              ...options,
              _retried: true,
              header: { ...header, "access-token": newToken },
            });
          } catch (_loginErr) {
            throw err;
          }
        }
      }
      throw err;
    }
  };

  // 提供便捷方法
  request.get = (url, data, options = {}) =>
    request({ url, method: "GET", data, ...options });

  request.post = (url, data, options = {}) =>
    request({ url, method: "POST", data, ...options });

  request.put = (url, data, options = {}) =>
    request({ url, method: "PUT", data, ...options });

  request.delete = (url, data, options = {}) =>
    request({ url, method: "DELETE", data, ...options });

  return request;
}

// 默认请求实例 — 指向千岛 OpenAPI
export const api = createRequest(OPENAPI_BASE);

// 后端服务请求实例 — 指向 FC 函数
export const backendApi = createRequest(BACKEND_BASE);
