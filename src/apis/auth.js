import Taro from "@tarojs/taro";
import {
  APP_ID,
  APP_SECRET,
  AUTH_BASE,
  AUTH_TOKEN_PATH,
  AUTH_REFRESH_PATH,
  ACCESS_TOKEN_KEY,
  REFRESH_TOKEN_KEY,
  EXPIRES_AT_KEY,
} from "./config";

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

// ─── 用 code 换取 token ───
async function exchangeCodeForToken(code) {
  const res = await Taro.request({
    url: `${AUTH_BASE}${AUTH_TOKEN_PATH}`,
    method: "POST",
    header: { "content-type": "application/json" },
    data: {
      appId: APP_ID,
      appSecret: APP_SECRET,
      code,
      grantType: "AUTHORIZATION_CODE",
    },
  });
  return res;
}

// ─── 登录：qd.login → 换取 accessToken ───
export async function doLogin() {
  let loginRes = await new Promise((resolve, reject) => {
    Taro.login({
      success: (res) => resolve(res),
      fail: (err) => reject(err),
    });
  });
  let code = loginRes.code;

  let res = await exchangeCodeForToken(code);
  let json = res?.data ?? res;

  // code 过期/无效时，重新获取 code 再试一次
  if (
    json &&
    String(json.code) !== "0" &&
    /code|授权码|过期|expired|invalid/i.test(json.message || "")
  ) {
    loginRes = await new Promise((resolve, reject) => {
      Taro.login({
        success: (res) => resolve(res),
        fail: (err) => reject(err),
      });
    });
    code = loginRes.code;

    res = await exchangeCodeForToken(code);
    json = res?.data ?? res;
  }

  if (!json || String(json.code) !== "0" || !json.data) {
    const msg = json?.message || json?.errMsg || "登录失败";
    throw new Error(msg);
  }

  saveTokens(
    json.data.accessToken,
    json.data.refreshToken,
    json.data.expiresAt,
  );
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
    refreshing = refreshToken().finally(() => {
      refreshing = null;
    });
  }
  return refreshing;
}
