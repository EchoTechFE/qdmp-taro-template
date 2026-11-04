import Taro from "@tarojs/taro";
import { getBackendBase } from "./backend";
import { ACCESS_TOKEN_KEY, tokenNeedsRefresh, ensureRefresh } from "./auth";

const AUTH_REFRESH_PATH = "/auth/v1/refresh";

/**
 * 创建带 access-token 自动注入的请求实例
 * @param {string} baseURL - 接口域名
 */
export function createRequest(baseURL) {
  const request = async (options) => {
    const { url, method = "GET", data, header = {}, ...rest } = options;

    const fullUrl = url.startsWith("http")
      ? url
      : `${typeof baseURL === "function" ? baseURL() : baseURL}${url}`;
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

      return res.data;
    } catch (err) {
      const status = err?.statusCode ?? err?.status;

      if (status === 401 && !isRefreshCall && !options._retried) {
        try {
          const refreshData = await ensureRefresh();
          const newToken = refreshData.accessToken || Taro.getStorageSync(ACCESS_TOKEN_KEY);
          return request({
            ...options,
            _retried: true,
            header: { ...header, "access-token": newToken },
          });
        } catch (e) {
          throw err;
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
export const api = createRequest("https://openapi.qiandao.com");

// 后端服务请求实例 — 域名来自构建时准备的公开 AppID。
export const backendApi = createRequest(getBackendBase);
