import { api, backendApi } from "./request";

/**
 * 搜索 SPU
 */
export async function searchSpu({ keyword = "", offset = 0, limit = 10 } = {}) {
  const params = { offset, limit };
  if (keyword) params.keyword = keyword;

  const query = Object.entries(params)
    .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
    .join("&");

  const json = await backendApi.get(`/spu/v1/search?${query}`);

  if (String(json.code) === "0") {
    return json.data;
  }
  throw new Error(json.message || "搜索失败");
}

/**
 * 获取 SPU 详情
 */
export async function getSpuDetail(id) {
  const json = await api.get(`/spu/v1/detail?id=${encodeURIComponent(id)}`);

  if (String(json.code) === "0") {
    return json.data.spu || json.data.librarySpu || json.data;
  }
  throw new Error(json.message || "获取详情失败");
}
