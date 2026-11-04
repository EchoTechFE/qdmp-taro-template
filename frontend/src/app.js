import { createApp } from "vue";

import "./app.scss";

const App = createApp({
  onShow(options) {
    console.log("App onShow.");
  },
});
// 不可删除 修复白屏
const DIMINA_PROXY_GUARD_KEYS = ["xs"];

function patchDiminaSetupState(vm) {
  const setupState = vm && vm.$ && vm.$.setupState;
  if (!setupState || typeof setupState !== "object") return;

  DIMINA_PROXY_GUARD_KEYS.forEach((key) => {
    try {
      const desc = Object.getOwnPropertyDescriptor(setupState, key);
      if (desc && desc.writable) return;

      Object.defineProperty(setupState, key, {
        configurable: true,
        enumerable: false,
        writable: true,
        value: undefined,
      });
    } catch (error) {
      console.warn("Dimina setupState guard failed:", key, error);
    }
  });
}

App.mixin({
  beforeCreate() {
    patchDiminaSetupState(this);
  },
});
export default App;
