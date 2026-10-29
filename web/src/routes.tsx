import type { Component } from "solid-js";
import Login from "./pages/Login";
import Home from "./pages/Home";
import Explore from "./pages/Explore";
import FleetRedirect from "./pages/FleetRedirect";
import Locations from "./pages/Locations";
import Systems from "./pages/Systems";
import Components from "./pages/Components";
import Profile from "./pages/Profile";
import Subscriptions from "./pages/Subscriptions";
import Inbox from "./pages/Inbox";
import Nodes from "./pages/Nodes";
import Simulation from "./pages/Simulation";
import Users from "./pages/Users";
import Roles from "./pages/Roles";
import Groups from "./pages/Groups";
import Secrets from "./pages/Secrets";
import Variables from "./pages/Variables";
import Metrics from "./pages/Metrics";
import Properties from "./pages/Properties";
import EventTypes from "./pages/EventTypes";
import CommandTypes from "./pages/CommandTypes";
import Notifications from "./pages/Notifications";
import NotificationTargets from "./pages/NotificationTargets";
import ActionRules from "./pages/ActionRules";
import Rules from "./pages/Rules";
import CalcRules from "./pages/CalcRules";
import SeverityLevels from "./pages/SeverityLevels";
import Tags from "./pages/Tags";
import CatalogOverview from "./pages/CatalogOverview";
import LocationTypes from "./pages/LocationTypes";
import ComponentTypes from "./pages/ComponentTypes";
import SystemTypes from "./pages/SystemTypes";
import SecretTypes from "./pages/SecretTypes";
import Standards from "./pages/Standards";
import Templates from "./pages/Templates";
import Vendors from "./pages/Vendors";
import Drivers from "./pages/Drivers";
import Products from "./pages/Products";
import Files from "./pages/Files";
import Audit from "./pages/Audit";
import Settings from "./pages/Settings";
import SectionStub from "./pages/SectionStub";
import NotFound from "./pages/NotFound";

// The page registry the route manifest resolves against: routes.tsx and the
// manifest (lib/routemanifest.ts, pure data) together are the router's source,
// and route-manifest-guard.test.ts pins that every manifest entry names a key
// here. Separate from index.tsx so tests can import the registry without
// index.tsx's render side effect.
export const PAGES: Record<string, Component> = {
  Login,
  Home,
  Explore,
  FleetRedirect,
  Locations,
  Systems,
  Components,
  Profile,
  Subscriptions,
  Inbox,
  Nodes,
  Simulation,
  Users,
  Roles,
  Groups,
  Secrets,
  Variables,
  Metrics,
  Properties,
  EventTypes,
  CommandTypes,
  Notifications,
  NotificationTargets,
  ActionRules,
  Rules,
  CalcRules,
  SeverityLevels,
  Tags,
  CatalogOverview,
  LocationTypes,
  ComponentTypes,
  SystemTypes,
  SecretTypes,
  Standards,
  Templates,
  Vendors,
  Drivers,
  Products,
  Files,
  Audit,
  Settings,
  SectionStub,
  NotFound,
};
