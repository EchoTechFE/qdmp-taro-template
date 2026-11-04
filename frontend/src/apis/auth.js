import Taro from "@tarojs/taro";

import { getBackendBase } from "./backend";

// Configure appId and appSecret in the project-root qdmp-config.json.
// The backend reads those credentials; the client sends only the login code.
const AUTH_BASE = "https://openapi.qiandao.com";
const AUTH_TOKEN_PATH = "/auth/v1/token";
const AUTH_REFRESH_PATH = "/auth/v1/refresh";

// ─── 存储 Key ───
export const ACCESS_TOKEN_KEY = "QDMP_ACCESS_TOKEN";
export const REFRESH_TOKEN_KEY = "QDMP_REFRESH_TOKEN";
export const EXPIRES_AT_KEY = "QDMP_TOKEN_EXPIRES_AT";

// ─── Token 存取 ───
function saveTokens(accessToken, refreshToken, expiresAt) {
  Taro.setStorageSync(ACCESS_TOKEN_KEY, accessToken);
  if (refreshToken) Taro.setStorageSync(REFRESH_TOKEN_KEY, refreshToken);
  if (expiresAt != null) Taro.setStorageSync(EXPIRES_AT_KEY, String(expiresAt));
}

export function clearTokens() {
  Taro.removeStorageSync(ACCESS_TOKEN_KEY);
  Taro.removeStorageSync(REFRESH_TOKEN_KEY);
  Taro.removeStorageSync(EXPIRES_AT_KEY);
}

export function getAccessToken() {
  return Taro.getStorageSync(ACCESS_TOKEN_KEY) || "";
}

export function isExpired() {
  const exp = parseInt(Taro.getStorageSync(EXPIRES_AT_KEY), 10) || 0;
  if (!exp) return false;
  return Math.floor(Date.now() / 1000) >= exp;
}

export function tokenNeedsRefresh() {
  const exp = parseInt(Taro.getStorageSync(EXPIRES_AT_KEY), 10) || 0;
  if (!exp) return false;
  return exp - Math.floor(Date.now() / 1000) < 300;
}

// ─── 静默登录：Taro.login → 换取 accessToken ───
export async function doLogin() {
  const backendBase = getBackendBase();
  const loginRes = await Taro.login();
  const code = loginRes.code;

  const res = await Taro.request({
    url: `${backendBase}${AUTH_TOKEN_PATH}`,
    method: "POST",
    header: { "content-type": "application/json" },
    data: { code },
  });

  const json = res?.data ?? res;
  if (!json || String(json.code) !== "0" || !json.data) {
    const msg = json?.message || json?.errMsg || JSON.stringify(json) || "登录失败";
    throw new Error(typeof msg === "string" ? msg : JSON.stringify(msg));
  }

  saveTokens(json.data.accessToken, json.data.refreshToken, json.data.expiresAt);
  return json.data;
}

// ─── 刷新 Token ───
async function refreshToken() {
  const rt = Taro.getStorageSync(REFRESH_TOKEN_KEY);
  if (!rt) return doLogin();

  const res = await Taro.request({
    url: `${AUTH_BASE}${AUTH_REFRESH_PATH}`,
    method: "POST",
    header: { "content-type": "application/json" },
    data: { refreshToken: rt },
  });

  const json = res?.data ?? res;

  if (!json || String(json.code) !== "0" || !json.data) {
    clearTokens();
    return doLogin();
  }

  saveTokens(json.data.accessToken, rt, json.data.expiresAt);
  return json.data;
}

// ─── 并发安全的刷新入口 ───
let refreshing = null;

export function ensureRefresh() {
  if (!refreshing) {
    refreshing = refreshToken().finally(() => { refreshing = null; });
  }
  return refreshing;
}
