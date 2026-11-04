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
export const OPEN_ID_KEY = "QDMP_OPEN_ID";

let sessionGeneration = 0;
let refreshing = null;

const nonemptyString = value => typeof value === "string" && value.trim() !== "";
const validExpiry = value => (typeof value === "number" || typeof value === "string")
  && Number.isSafeInteger(Number(value)) && Number(value) > 0;

function captureSession() {
  return {
    generation: sessionGeneration,
    accessToken: Taro.getStorageSync(ACCESS_TOKEN_KEY) || "",
    refreshToken: Taro.getStorageSync(REFRESH_TOKEN_KEY) || "",
    expiresAt: Taro.getStorageSync(EXPIRES_AT_KEY) || "",
    openId: Taro.getStorageSync(OPEN_ID_KEY) || "",
  };
}

function isCurrentSession(session) {
  const current = captureSession();
  return Object.keys(current).every(key => current[key] === session[key]);
}

function assertCurrentSession(session) {
  if (!isCurrentSession(session)) throw new Error("登录会话已改变，请重新发起请求");
}

function hasCompleteSession(session) {
  return [session.accessToken, session.refreshToken, session.openId].every(nonemptyString)
    && validExpiry(session.expiresAt);
}

function readSession() {
  const session = captureSession();
  if (hasCompleteSession(session)) return session;
  if ([session.accessToken, session.refreshToken, session.expiresAt, session.openId].some(Boolean)) clearTokens();
  return null;
}

function responseData(res, message) {
  const json = res?.data;
  if (!Number.isInteger(res?.statusCode) || res.statusCode < 200 || res.statusCode >= 300
    || (json?.code !== 0 && json?.code !== "0") || !json.data || typeof json.data !== "object" || Array.isArray(json.data)) {
    throw new Error(nonemptyString(json?.message) ? json.message : message);
  }
  return json.data;
}

// ─── Token 存取 ───
function saveSession(session) {
  // Validate the entire authorization-code result before writing any field.
  if (![session.accessToken, session.refreshToken, session.openId].every(nonemptyString)
    || !validExpiry(session.expiresAt)) throw new Error("登录换票响应不完整");
  try {
    Taro.setStorageSync(ACCESS_TOKEN_KEY, session.accessToken);
    Taro.setStorageSync(REFRESH_TOKEN_KEY, session.refreshToken);
    Taro.setStorageSync(EXPIRES_AT_KEY, String(Number(session.expiresAt)));
    Taro.setStorageSync(OPEN_ID_KEY, session.openId);
    sessionGeneration += 1;
  } catch (error) {
    clearTokens();
    throw error;
  }
}

export function clearTokens() {
  sessionGeneration += 1;
  refreshing = null;
  Taro.removeStorageSync(ACCESS_TOKEN_KEY);
  Taro.removeStorageSync(REFRESH_TOKEN_KEY);
  Taro.removeStorageSync(EXPIRES_AT_KEY);
  Taro.removeStorageSync(OPEN_ID_KEY);
}

export function getAccessToken() {
  // Legacy token-only caches must obtain openId through a new login exchange.
  return readSession()?.accessToken || "";
}

export function getOpenId() {
  return readSession()?.openId || "";
}

export function isExpired() {
  const session = readSession();
  return !session || Math.floor(Date.now() / 1000) >= Number(session.expiresAt);
}

export function tokenNeedsRefresh() {
  const session = readSession();
  return !session || Number(session.expiresAt) - Math.floor(Date.now() / 1000) < 300;
}

// ─── 静默登录：Taro.login → 换取 accessToken ───
export async function doLogin() {
  return loginSession();
}

async function loginSession(pendingRefresh) {
  const backendBase = getBackendBase();
  clearTokens();
  const session = captureSession();
  if (pendingRefresh) {
    pendingRefresh.session = session;
    refreshing = pendingRefresh;
  }
  const loginRes = await Taro.login();
  assertCurrentSession(session);
  const code = loginRes?.code;
  if (!nonemptyString(code)) throw new Error("缺少有效登录 code");

  const res = await Taro.request({
    url: `${backendBase}${AUTH_TOKEN_PATH}`,
    method: "POST",
    header: { "content-type": "application/json" },
    data: { code },
  });

  assertCurrentSession(session);
  const data = responseData(res, "登录失败");
  // openId is accepted only from this AUTHORIZATION_CODE exchange, never user.me.
  saveSession(data);
  return { ...data, expiresAt: Number(data.expiresAt) };
}

// ─── 刷新 Token ───
async function refreshToken(pendingRefresh) {
  const session = captureSession();
  if (!hasCompleteSession(session)) return loginSession(pendingRefresh);

  const res = await Taro.request({
    url: `${AUTH_BASE}${AUTH_REFRESH_PATH}`,
    method: "POST",
    header: { "content-type": "application/json" },
    data: { refreshToken: session.refreshToken },
  });

  assertCurrentSession(session);
  let data;
  try {
    data = responseData(res, "刷新 Token 失败");
    if (!nonemptyString(data.accessToken) || !validExpiry(data.expiresAt)) throw new Error("刷新 Token 响应不完整");
  } catch {
    return loginSession(pendingRefresh);
  }
  // Refresh cannot establish a new user identity or rotate the original token.
  try {
    Taro.setStorageSync(ACCESS_TOKEN_KEY, data.accessToken);
    Taro.setStorageSync(EXPIRES_AT_KEY, String(Number(data.expiresAt)));
  } catch (error) {
    clearTokens();
    throw error;
  }
  return { ...data, expiresAt: Number(data.expiresAt), refreshToken: session.refreshToken, openId: session.openId };
}

// ─── 并发安全的刷新入口 ───
export function ensureRefresh() {
  if (refreshing && isCurrentSession(refreshing.session)) return refreshing.promise;
  const pending = { session: captureSession() };
  pending.promise = Promise.resolve().then(() => {
    assertCurrentSession(pending.session);
    return refreshToken(pending);
  }).finally(() => { if (refreshing === pending) refreshing = null; });
  refreshing = pending;
  return pending.promise;
}
