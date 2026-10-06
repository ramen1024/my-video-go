import { mount } from "svelte";
import App from "./App.svelte";

const target = document.getElementById("app");
if (!target) {
  throw new Error("挂载点 #app 不存在");
}

const app = mount(App, { target });

export default app;
