import { expect, it } from "vitest";
import { routes } from "./router";

it("builds only the documented read navigation routes", () => {
  expect(routes.map((route) => route.path)).toEqual([
    "/",
    "/initiatives/:initiative",
    "/initiatives/:initiative/epics/:epic",
    "/initiatives/:initiative/epics/:epic/packets/:packet",
    "/signed-out",
    "/:pathMatch(.*)*",
  ]);
});
