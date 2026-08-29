import { createApp } from "vue";
import App from "./App.vue";
import { apiClient } from "./api";
import { createFirebaseAuth } from "./auth";
import { makeRouter } from "./router";
import "./styles.css";

const auth = await createFirebaseAuth();
const router = makeRouter();
createApp(App, { auth, api: apiClient }).use(router).mount("#app");
