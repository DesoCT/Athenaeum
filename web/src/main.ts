import { mount } from "svelte";
import App from "./App.svelte";
import "./styles/app.css";
import { applyEnvironment, watchSystemTheme } from "./settings/settings.svelte";

// Apply the theme and interface size before mounting so there is no flash of the
// wrong theme, and keep a "system" theme in step with the OS.
applyEnvironment();
watchSystemTheme();

const target = document.getElementById("app");
if (!target) {
  throw new Error("Athenaeum could not find its mount point (#app).");
}

export default mount(App, { target });
