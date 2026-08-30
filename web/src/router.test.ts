import { expect, it } from "vitest";
import { routes } from "./router";

it("builds the documented read and draft-authoring navigation routes", () => {
  expect(routes.map((route) => route.path)).toEqual([
    "/",
    "/initiatives/:initiative",
    "/initiatives/:initiative/epics/:epic",
    "/initiatives/:initiative/epics/:epic/new",
    "/initiatives/:initiative/epics/:epic/packets/:packet",
    "/signed-out",
    "/:pathMatch(.*)*",
  ]);
});
