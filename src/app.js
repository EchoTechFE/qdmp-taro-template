import { createApp } from "vue";

import "./app.scss";

const App = createApp({
  onShow(options) {
    console.log("App onShow.");
  },
});
// Workaround: dimina pageFrame.js 的 Proxy getOwnPropertyDescriptor trap
// 对不存在的属性返回 { writable: false } 的描述符，导致写入 xs 时抛 TypeError 白屏。
// 预声明 xs 使其成为 setupState 上的真实属性，绕过伪造描述符分支。
App.mixin({
  setup() {
    return { xs: undefined };
  },
});
export default App;
