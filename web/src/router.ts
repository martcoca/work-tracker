import { createRouter, createWebHistory, type RouteRecordRaw } from "vue-router";

const EmptyRoute = { template: "<span />" };

export const routes: RouteRecordRaw[] = [
  { path: "/", name: "initiatives", component: EmptyRoute },
  { path: "/initiatives/:initiative", name: "initiative", component: EmptyRoute },
  { path: "/initiatives/:initiative/epics/:epic", name: "epic", component: EmptyRoute },
  {
    path: "/initiatives/:initiative/epics/:epic/packets/:packet",
    name: "packet",
    component: EmptyRoute,
  },
  { path: "/signed-out", name: "signed-out", component: EmptyRoute },
  { path: "/:pathMatch(.*)*", name: "not-found", component: EmptyRoute },
];

export function makeRouter() {
  return createRouter({ history: createWebHistory(), routes });
}
