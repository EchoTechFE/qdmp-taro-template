import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";

const source = readFileSync(new URL("../src/apis/auth.js", import.meta.url), "utf8");
const requestSource = readFileSync(new URL("../src/apis/request.js", import.meta.url), "utf8");
const exports = ["ACCESS_TOKEN_KEY", "REFRESH_TOKEN_KEY", "EXPIRES_AT_KEY", "OPEN_ID_KEY", "clearTokens", "getAccessToken", "getOpenId", "isExpired", "tokenNeedsRefresh", "doLogin", "ensureRefresh"];
const stripModule = text => text.replace(/^import .*;\s*$/gm, "").replace(/\bexport /g, "");
const future = () => Math.floor(Date.now() / 1000) + 3600;
const session = (id = "a") => ({ accessToken: `access-${id}`, refreshToken: `refresh-${id}`, expiresAt: future(), openId: `openid-${id}` });
const response = (data = session(), statusCode = 200, code = 0) => ({ statusCode, data: { code, data } });
const plain = value => JSON.parse(JSON.stringify(value));
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
const tick = async () => { for (let index = 0; index < 6; index++) await Promise.resolve(); };

function fixture({ request = async () => response(), login = async () => ({ code: "native-login-code" }), failStorage } = {}) {
  const storage = new Map(), requests = [], writes = [];
  let logins = 0;
  const Taro = {
    getStorageSync: key => storage.get(key) ?? "",
    setStorageSync: (key, value) => { if (key === failStorage) throw new Error("synthetic storage failure"); writes.push([key, value]); storage.set(key, value); },
    removeStorageSync: key => storage.delete(key),
    login: async () => { logins += 1; return login(logins); },
    request: async options => { requests.push(options); return request(options, requests.length); },
  };
  const context = vm.createContext({ Taro, getBackendBase: () => "https://synthetic-app.qiandaomp.com", console: { warn() {} } });
  vm.runInContext(`${stripModule(source)}\nglobalThis.auth = { ${exports.join(",")} };`, context, { filename: "auth.js" });
  vm.runInContext(`(() => { ${stripModule(requestSource)}\nglobalThis.requests = { createRequest, api, backendApi }; })();`, context, { filename: "request.js" });
  const auth = context.auth;
  const seed = value => {
    for (const [field, key] of [["accessToken", auth.ACCESS_TOKEN_KEY], ["refreshToken", auth.REFRESH_TOKEN_KEY], ["expiresAt", auth.EXPIRES_AT_KEY], ["openId", auth.OPEN_ID_KEY]]) {
      if (value[field] !== undefined) storage.set(key, field === "expiresAt" ? String(value[field]) : value[field]);
    }
  };
  const current = () => ({ accessToken: storage.get(auth.ACCESS_TOKEN_KEY), refreshToken: storage.get(auth.REFRESH_TOKEN_KEY), expiresAt: Number(storage.get(auth.EXPIRES_AT_KEY)), openId: storage.get(auth.OPEN_ID_KEY) });
  return { auth, modules: context.requests, storage, requests, writes, seed, current, logins: () => logins };
}

test("authorization-code login validates and persists the complete identity, sends only code", async () => {
  const expected = session();
  const f = fixture({ request: async () => response({ ...expected, expiresAt: String(expected.expiresAt) }) });
  for (const name of exports) assert.notEqual(f.auth[name], undefined, name);
  const actual = await f.auth.doLogin();
  assert.deepEqual(plain(actual), expected);
  assert.deepEqual(f.current(), expected);
  assert.equal(f.auth.getOpenId(), expected.openId);
  assert.equal(f.auth.getAccessToken(), expected.accessToken);
  assert.equal(f.requests.length, 1);
  assert.equal(f.requests[0].url, "https://synthetic-app.qiandaomp.com/auth/v1/token");
  assert.deepEqual(plain(f.requests[0].data), { code: "native-login-code" });
});

test("HTTP/business failures and incomplete authorization-code success never write a partial session", async t => {
  const good = session();
  const cases = [
    ["http failure", response(good, 503)], ["missing HTTP status", { data: { code: 0, data: good } }],
    ["business failure", response(good, 200, 10001)], ["nonliteral code", response(good, 200, [0])],
    ["missing data", response(null)], ["array data", response([])],
    ...["accessToken", "refreshToken", "openId"].flatMap(field => [
      [`missing ${field}`, response(Object.fromEntries(Object.entries(good).filter(([key]) => key !== field)))],
      [`empty ${field}`, response({ ...good, [field]: "  " })],
      [`nonstring ${field}`, response({ ...good, [field]: 1 })],
    ]),
    ...[undefined, null, true, 0, -1, 1.5, Number.NaN, Infinity, Number.MAX_SAFE_INTEGER + 1, "invalid", [], {}].map(expiresAt => [`invalid expiry ${String(expiresAt)}`, response({ ...good, expiresAt })]),
  ];
  for (const [name, result] of cases) await t.test(name, async () => {
    const f = fixture({ request: async () => result });
    f.seed(session("previous"));
    await assert.rejects(f.auth.doLogin());
    assert.equal(f.writes.length, 0);
    assert.equal(f.storage.size, 0);
    assert.equal(f.requests.length, 1);
    assert.equal(f.requests.some(call => /user\/v1\/me/.test(call.url)), false);
  });
});

test("invalid native login code stops before token exchange", async () => {
  const f = fixture({ login: async () => ({ code: " " }) });
  await assert.rejects(f.auth.doLogin(), /code/);
  assert.equal(f.requests.length, 0);
  assert.equal(f.storage.size, 0);
});

test("legacy token cache without openId is cleared and the original login flow obtains a new identity", async () => {
  const f = fixture();
  const { openId, ...legacy } = session("legacy");
  f.seed(legacy);
  assert.equal(f.auth.getAccessToken(), "");
  assert.equal(f.storage.size, 0);
  await f.auth.doLogin();
  assert.equal(f.auth.getOpenId(), "openid-a");
  assert.equal(f.logins(), 1);
  assert.equal(f.requests.length, 1);
  assert.match(f.requests[0].url, /\/auth\/v1\/token$/);
});

test("legacy cache detected during refresh relogs once and concurrent consumers share it", async () => {
  const f = fixture();
  const { openId, ...legacy } = session("legacy");
  f.seed(legacy);
  assert.equal(f.auth.tokenNeedsRefresh(), true);
  assert.equal(f.storage.size, 0);
  const first = f.auth.ensureRefresh(), second = f.auth.ensureRefresh();
  assert.equal(first, second);
  await first;
  assert.equal(f.logins(), 1);
  assert.equal(f.requests.length, 1);
  assert.match(f.requests[0].url, /\/auth\/v1\/token$/);
});

test("refresh calls OpenAPI directly, changes only access/expiry and preserves exchange identity", async () => {
  const original = session("original");
  const next = { accessToken: "refreshed-access", expiresAt: original.expiresAt + 100, refreshToken: "untrusted-rotation", openId: "untrusted-user" };
  const f = fixture({ request: async () => response(next) });
  f.seed(original);
  const first = f.auth.ensureRefresh(), second = f.auth.ensureRefresh();
  assert.equal(first, second);
  const result = await first;
  assert.equal(f.logins(), 0);
  assert.equal(f.requests.length, 1);
  assert.equal(f.requests[0].url, "https://openapi.qiandao.com/auth/v1/refresh");
  assert.deepEqual(plain(f.requests[0].data), { refreshToken: original.refreshToken });
  assert.deepEqual(f.current(), { ...original, accessToken: next.accessToken, expiresAt: next.expiresAt });
  assert.equal(result.openId, original.openId);
  assert.equal(result.refreshToken, original.refreshToken);
  assert.deepEqual(f.writes.map(([key]) => key), [f.auth.ACCESS_TOKEN_KEY, f.auth.EXPIRES_AT_KEY]);
});

test("clearTokens removes every session field and cancels a queued refresh before any HTTP call", async () => {
  const f = fixture();
  f.seed(session());
  const pending = f.auth.ensureRefresh();
  f.auth.clearTokens();
  await assert.rejects(pending, /会话已改变/);
  assert.equal(f.storage.size, 0);
  assert.equal(f.requests.length, 0);
});

for (const outcome of ["success", "business failure", "network failure"]) {
  test(`late refresh ${outcome} cannot overwrite or clear a newer login`, async () => {
    const old = deferred(), latest = session("new-user");
    const f = fixture({ request: options => options.url.endsWith("/refresh") ? old.promise : Promise.resolve(response(latest)) });
    f.seed(session("old-user"));
    const stale = f.auth.ensureRefresh();
    await tick();
    await f.auth.doLogin();
    const rejected = assert.rejects(stale);
    if (outcome === "network failure") old.reject(new Error("synthetic network failure"));
    else old.resolve(response(session("late"), outcome === "success" ? 200 : 503));
    await rejected;
    assert.deepEqual(f.current(), latest);
    assert.equal(f.logins(), 1);
    assert.equal(f.requests.length, 2);
  });
}

test("late refresh success cannot restore a logged-out session", async () => {
  const old = deferred();
  const f = fixture({ request: () => old.promise });
  f.seed(session());
  const pending = f.auth.ensureRefresh();
  await tick();
  f.auth.clearTokens();
  const rejected = assert.rejects(pending, /会话已改变/);
  old.resolve(response(session("late")));
  await rejected;
  assert.equal(f.storage.size, 0);
});

for (const outcome of ["success", "business failure", "network failure"]) {
  test(`late login ${outcome} cannot overwrite or clear a newer user session`, async () => {
    const old = deferred(), latest = session("latest");
    const f = fixture({ request: (_options, count) => count === 1 ? old.promise : Promise.resolve(response(latest)) });
    const stale = f.auth.doLogin();
    await tick();
    await f.auth.doLogin();
    const rejected = assert.rejects(stale);
    if (outcome === "network failure") old.reject(new Error("synthetic network failure"));
    else old.resolve(response(session("late"), outcome === "success" ? 200 : 503));
    await rejected;
    assert.deepEqual(f.current(), latest);
  });
}

test("logout during native code acquisition prevents a later token exchange", async () => {
  const native = deferred();
  const f = fixture({ login: () => native.promise });
  const pending = f.auth.doLogin();
  f.auth.clearTokens();
  const rejected = assert.rejects(pending, /会话已改变/);
  native.resolve({ code: "late-code" });
  await rejected;
  assert.equal(f.requests.length, 0);
  assert.equal(f.storage.size, 0);
});

test("old refresh cleanup cannot release a newer user's in-flight refresh", async () => {
  const old = deferred(), current = deferred();
  const f = fixture({ request: (options, count) => options.url.endsWith("/token") ? Promise.resolve(response(session("new"))) : count === 1 ? old.promise : current.promise });
  f.seed(session("old"));
  const first = f.auth.ensureRefresh();
  await tick();
  await f.auth.doLogin();
  const second = f.auth.ensureRefresh();
  await tick();
  const rejected = assert.rejects(first);
  old.resolve(response(session("stale")));
  await rejected;
  assert.equal(f.auth.ensureRefresh(), second);
  current.resolve(response({ accessToken: "latest-refresh", expiresAt: future() }));
  await second;
  assert.equal(f.auth.getOpenId(), "openid-new");
  assert.equal(f.auth.getAccessToken(), "latest-refresh");
});

test("invalid current refresh falls back to one deduplicated login without trusting refresh identity", async () => {
  const native = deferred();
  const f = fixture({ login: () => native.promise, request: options => Promise.resolve(options.url.endsWith("/refresh") ? response({ accessToken: "", expiresAt: future() }) : response(session("relogin"))) });
  f.seed(session("old"));
  const first = f.auth.ensureRefresh();
  await tick();
  assert.equal(f.auth.ensureRefresh(), first);
  assert.equal(f.logins(), 1);
  assert.equal(f.storage.size, 0);
  native.resolve({ code: "replacement-code" });
  await first;
  assert.equal(f.auth.getOpenId(), "openid-relogin");
});

test("a storage failure rolls back the complete login session", async () => {
  const f = fixture({ failStorage: "QDMP_OPEN_ID" });
  await assert.rejects(f.auth.doLogin(), /storage failure/);
  assert.equal(f.storage.size, 0);
});

test("ordinary requests never attach a legacy token without openId and preserve original HTTP response semantics", async () => {
  const f = fixture({ request: async () => ({ statusCode: 403, data: { code: 403, message: "synthetic denied" } }) });
  const { openId, ...legacy } = session("legacy");
  f.seed(legacy);
  const result = await f.modules.api.get("/synthetic-business");
  assert.equal(result.code, 403);
  assert.equal(f.requests.length, 1);
  assert.equal(f.requests[0].header["access-token"], undefined);
  assert.equal(f.storage.size, 0);
  assert.equal(f.logins(), 0);
});

for (const { retry, switchAccount, refreshFails } of [
  { retry: false, switchAccount: false, refreshFails: false },
  { retry: false, switchAccount: true, refreshFails: true },
  { retry: true, switchAccount: false, refreshFails: true },
  { retry: true, switchAccount: true, refreshFails: false },
]) {
  const trigger = retry ? "401 retry" : "proactive refresh";
  const action = switchAccount ? "account switch" : "logout";
  test(`${trigger} cannot send a business request after ${action}`, async () => {
    const refresh = deferred();
    const replacement = session("replacement");
    const businessCalls = [];
    const f = fixture({ request: async options => {
      if (options.url.endsWith("/refresh")) return refresh.promise;
      if (options.url.endsWith("/token")) return response(replacement);
      businessCalls.push(options);
      throw Object.assign(new Error("synthetic unauthorized"), { statusCode: 401 });
    } });
    const original = session("original");
    f.seed({ ...original, expiresAt: retry ? original.expiresAt : future() - 3540 });
    const pending = f.modules.backendApi.post("/synthetic-business", { title: "sample" });
    const rejected = assert.rejects(pending, /会话已改变/);
    await tick();
    assert.equal(f.requests.filter(call => call.url.endsWith("/refresh")).length, 1);

    if (switchAccount) await f.auth.doLogin();
    else f.auth.clearTokens();
    if (refreshFails) refresh.reject(new Error("synthetic network failure"));
    else refresh.resolve(response({ accessToken: "stale-refresh", expiresAt: future() }));

    await rejected;
    assert.deepEqual(businessCalls.map(call => call.header["access-token"]), retry ? [original.accessToken] : []);
    if (switchAccount) assert.deepEqual(f.current(), replacement);
    else assert.equal(f.storage.size, 0);
  });
}

for (const sameAccount of [true, false]) {
  test(`refresh fallback only continues the original business request for the same account (${sameAccount})`, async () => {
    const original = session("original");
    const replacement = session("replacement");
    const businessCalls = [];
    const f = fixture({ request: async options => {
      if (options.url.endsWith("/refresh")) return response(null, 401, 401);
      if (options.url.endsWith("/token")) {
        return response({ ...replacement, openId: sameAccount ? original.openId : replacement.openId });
      }
      businessCalls.push(options);
      return { statusCode: 200, data: { code: 0 } };
    } });
    f.seed({ ...original, expiresAt: future() - 3540 });

    const pending = f.modules.backendApi.post("/synthetic-business", { title: "sample" });
    if (sameAccount) assert.equal((await pending).code, 0);
    else await assert.rejects(pending, /会话已改变/);
    assert.deepEqual(businessCalls.map(call => call.header["access-token"]), sameAccount ? [replacement.accessToken] : []);
  });
}

test("a 401 retry in the same session sends the refreshed token once", async () => {
  const original = session("original");
  const businessTokens = [];
  const f = fixture({ request: async options => {
    if (options.url.endsWith("/refresh")) return response({ accessToken: "fresh-access", expiresAt: future() });
    businessTokens.push(options.header["access-token"]);
    if (businessTokens.length === 1) {
      throw Object.assign(new Error("synthetic unauthorized"), { statusCode: 401 });
    }
    return { statusCode: 200, data: { code: 0 } };
  } });
  f.seed(original);

  assert.equal((await f.modules.backendApi.post("/synthetic-business", { title: "sample" })).code, 0);
  assert.deepEqual(businessTokens, [original.accessToken, "fresh-access"]);
});
