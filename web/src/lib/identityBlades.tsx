import { type BladeDef } from "./blades";
import { userBlade } from "../pages/UserDetail";
import { groupBlade } from "../pages/GroupDetail";
import { roleBlade } from "../pages/RoleDetail";

// The identity console's cross-entity blade registry: a user blade can drill into a
// group and vice versa, bounded by the per-page root so the drill graph stays
// acyclic (Users roots user -> group; Groups roots group -> user; Roles is a
// read-only leaf). Every detail body self-fetches by id, so any kind renders from
// any page; the pages wire only the root->leaf drill direction.
export const identityRegistry: Record<string, BladeDef> = {
  user: userBlade,
  group: groupBlade,
  role: roleBlade,
};
