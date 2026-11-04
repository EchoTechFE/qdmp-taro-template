import appConfig from "../../qdmp.json";

// Only public frontend metadata is bundled. qdmp build prepares it from the
// private project-root configuration before compiling.
export function getBackendBase() {
  const appId = String(appConfig.appId || "").trim();
  if (!/^[A-Za-z0-9_-]{4,63}$/.test(appId)) {
    throw new Error("请先在项目根目录配置 appId，并执行 qdmp build");
  }
  return `https://${appId}.qiandaomp.com`;
}
