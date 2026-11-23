// The console's lessons (#2450): six guided walks through monitoring, in the
// order a fleet lives it, from a device becoming samples to reading the story
// afterwards. Each step is a few sentences, a LIVE element (a real console
// component over the simulated fleet, never a picture) and a link that opens
// the real surface with the right entity. These definitions are the single
// source for the Learn page and for the generated docs page
// (web/scripts/gen-lessons.mjs writes docs/src/generated/lessons.json under
// make gen), so the two cannot drift.
//
// This module stays PURE DATA plus pure functions, with no imports: the docs
// generator loads it in node, where a component import would not resolve.

// The live element a step renders, each one an existing console component:
// the simulated component's samples, the platform's delivery path, the reach
// ladder, a rule preview on a real metric, the component's alarms, the health
// of the systems it serves, its commands, and its activity.
export type LiveKind =
  | "samples"
  | "pipeline"
  | "simulation"
  | "reach"
  | "rule-preview"
  | "alarms"
  | "health"
  | "commands"
  | "activity";

// A surface a sibling slice is building. A step that would be better with it
// names it; until CAPABILITIES says it exists, the step keeps its main-today
// form, so the lesson never links to a page that is not there.
export type Capability =
  | "endpoint-test" // #2427: test an endpoint's poll from its row
  | "overlay-timeline" // #2425: the overlay timeline on a component
  | "sim-faults" // #2462: one-click simulated faults
  | "metrics-grid"; // #2424: the synced-cursor metrics grid

export const CAPABILITIES: Record<Capability, boolean> = {
  "endpoint-test": false,
  "overlay-timeline": false,
  "sim-faults": false,
  "metrics-grid": false,
};

// A link into the console. `to` is base-relative and may name {component},
// the simulated component the lesson is walked on, by uuid.
export type LessonLink = { label: string; to: string };

export type LessonStep = {
  title: string;
  prose: string;
  live?: LiveKind;
  link: LessonLink;
  // The better form of this step, once its sibling surface exists.
  better?: { needs: Capability; prose?: string; link: LessonLink };
};

export type Lesson = {
  slug: string;
  title: string;
  teaches: string;
  minutes: number;
  // True when the lesson's live elements and links need a simulated component.
  needsSimulator: boolean;
  steps: LessonStep[];
};

export const LESSONS: Lesson[] = [
  {
    slug: "device-to-samples",
    title: "From device to samples",
    teaches: "How collection turns a device on the network into samples the component owns, and where along the way work can be lost.",
    minutes: 5,
    needsSimulator: true,
    steps: [
      {
        title: "A component answers on the network",
        prose:
          "Every monitored device is a component. This one is simulated: a simulator beside the outpost answers on its address, speaking the product's protocols, so collection reads it as real hardware. The hits count each poll that reached it.",
        live: "simulation",
        link: { label: "Open the component", to: "/components/{component}" },
      },
      {
        title: "Each poll becomes a sample",
        prose:
          "The outpost polls the component on a schedule and turns each answer into samples: a number for a metric, a value for a property. The component owns them. Here are its metrics as they stand now, each with its latest value.",
        live: "samples",
        link: { label: "Read its vitals", to: "/components/{component}" },
        better: {
          needs: "metrics-grid",
          prose:
            "The outpost polls the component on a schedule and turns each answer into samples. The metrics grid charts every metric the component owns on one shared time cursor.",
          link: { label: "Open its metrics grid", to: "/components/{component}?tab=activity" },
        },
      },
      {
        title: "Delivery is measured hop by hop",
        prose:
          "Between the poll and the store, a sample crosses several hops. Each hop counts what came in and what went out, so loss is a ratio you read the same way everywhere: 0% is healthy. Pick a hop to see what it does.",
        live: "pipeline",
        link: { label: "Open Platform health", to: "/platform" },
      },
    ],
  },
  {
    slug: "reach-and-collection",
    title: "Reach and collection",
    teaches: "How Omniglass decides a device is reachable, rung by rung, and how to tell a network problem from a device problem.",
    minutes: 5,
    needsSimulator: true,
    steps: [
      {
        title: "The reach ladder",
        prose:
          "Reach is judged in rungs, in order: reachable (the address answers), responsive (the protocol speaks back), authenticated (the device accepts our credentials). The first rung that fails is where to look: a network problem stops low, a device or credential problem higher. Expand an endpoint to read its rungs.",
        live: "reach",
        link: { label: "Open its endpoints", to: "/components/{component}?tab=configure" },
        better: {
          needs: "endpoint-test",
          prose:
            "Reach is judged in rungs, in order: reachable, responsive, authenticated. Test an endpoint to run one poll now and see which rung answers.",
          link: { label: "Test an endpoint", to: "/components/{component}?tab=configure" },
        },
      },
      {
        title: "Break the network on purpose",
        prose:
          "A simulated component can be impaired: unplugged, slowed, or dropping packets. Open its simulation and add loss to watch the reach ladder and the delivery figures notice.",
        live: "simulation",
        link: { label: "Open the Simulation page", to: "/simulation" },
        better: {
          needs: "sim-faults",
          prose: "A simulated component can be impaired with one click: unplug it or drop its packets, and watch the reach ladder notice.",
          link: { label: "Open the Simulation page", to: "/simulation" },
        },
      },
    ],
  },
  {
    slug: "rules-and-alarms",
    title: "Rules and alarms",
    teaches: "How an event rule turns samples into an alarm: a threshold, a sustain so one bad sample never pages, and a clear.",
    minutes: 8,
    needsSimulator: true,
    steps: [
      {
        title: "Write a threshold, preview it",
        prose:
          "An event rule reads one metric or property and says when it is bad. Pick a metric this component reports, set a threshold, and preview: the rule runs against the last day of real samples and shows what it would have fired, writing nothing.",
        live: "rule-preview",
        link: { label: "Open the event rules", to: "/event-rules" },
      },
      {
        title: "Read a shipped rule",
        prose:
          "Omniglass ships rules for what every fleet needs. reachability-lost fires when a device stops answering and holds for 15 seconds first, so one missed poll never opens an alarm.",
        link: { label: "Open reachability-lost", to: "/event-rules?rule=reachability-lost" },
      },
      {
        title: "Trip it with a simulated fault",
        prose:
          "Open the simulation, tick Unplugged and save: the simulated component goes dark on every protocol, as if its cable were pulled.",
        live: "simulation",
        link: { label: "Unplug it from its page", to: "/components/{component}" },
        better: {
          needs: "sim-faults",
          prose: "Unplug the simulated component with one click: it goes dark on every protocol, as if its cable were pulled.",
          link: { label: "Open the Simulation page", to: "/simulation" },
        },
      },
      {
        title: "Watch the alarm open",
        prose:
          "The outpost finds the component down on its next poll, the rule waits out its 15 second sustain, and an alarm opens, listed here as it lands. Plug it back in the same way and the rule clears it.",
        live: "alarms",
        link: { label: "Open the alarm queue", to: "/alarms?state=active" },
      },
    ],
  },
  {
    slug: "health-and-impact",
    title: "Health and impact",
    teaches: "How an alarm moves health: the component's, then the system it serves, then the place the system sits.",
    minutes: 5,
    needsSimulator: true,
    steps: [
      {
        title: "An alarm moves the component",
        prose:
          "A component's health is the worst of its open alarms, by severity. A severity that takes a component out makes it down; a lesser one degrades it.",
        live: "alarms",
        link: { label: "Open the component", to: "/components/{component}" },
      },
      {
        title: "The component moves its systems",
        prose:
          "A system is the room's AV built to a standard, with slots its components fill. When a component in a slot is down, the slot is short and the system reads degraded or down. These are the systems this component serves.",
        live: "health",
        link: { label: "Open the fleet", to: "/explore" },
      },
    ],
  },
  {
    slug: "acting-on-it",
    title: "Acting on it",
    teaches: "What to do with an alarm: acknowledge it so the team knows it is owned, snooze it while you work, or send the device a command.",
    minutes: 5,
    needsSimulator: true,
    steps: [
      {
        title: "Acknowledge or snooze",
        prose:
          "Acknowledging says someone owns the alarm; it stays open until its rule clears it. Snoozing holds its notifications for a while without hiding it.",
        live: "alarms",
        link: { label: "Open the alarm queue", to: "/alarms?state=active" },
      },
      {
        title: "Issue a command",
        prose:
          "A command tells the device to do something, through its driver: power it on, switch an input. Each command is tracked until the device confirms it or it times out.",
        live: "commands",
        link: { label: "Issue one from its page", to: "/components/{component}" },
      },
    ],
  },
  {
    slug: "reading-the-history",
    title: "Reading the history",
    teaches: "How to read what happened afterwards: every event, alarm and command lands on the component's activity, in order.",
    minutes: 4,
    needsSimulator: true,
    steps: [
      {
        title: "The activity is the story",
        prose:
          "Every event a rule wrote, every alarm opened and cleared, and every command issued is kept on the component's activity, newest first. Read it bottom up to replay an incident.",
        live: "activity",
        link: { label: "Open its activity", to: "/components/{component}?tab=activity" },
        better: {
          needs: "overlay-timeline",
          prose:
            "The overlay timeline draws alarms, events and commands over the component's metrics on one time axis, so cause and effect line up.",
          link: { label: "Open its timeline", to: "/components/{component}?tab=activity" },
        },
      },
      {
        title: "Health over time",
        prose:
          "A system's health is recorded each time it changes, so its history shows how long it was degraded and when it recovered.",
        live: "health",
        link: { label: "Open the fleet", to: "/explore" },
      },
    ],
  },
];

// stepFor is the step as it renders under the given capabilities: its better
// form once the surface it needs exists, its main-today form until then.
export function stepFor(step: LessonStep, caps: Record<Capability, boolean> = CAPABILITIES): Omit<LessonStep, "better"> {
  const b = step.better;
  if (b && caps[b.needs]) return { title: step.title, prose: b.prose ?? step.prose, live: step.live, link: b.link };
  return { title: step.title, prose: step.prose, live: step.live, link: step.link };
}

// resolveLink fills a link's {component} with the component's uuid; with no
// component to fill it, a link that needs one resolves to null (the page then
// sends the learner to the lab instructions instead of a dead address).
export function resolveLink(to: string, ctx: { component?: string }): string | null {
  if (!to.includes("{component}")) return to;
  if (!ctx.component) return null;
  return to.split("{component}").join(encodeURIComponent(ctx.component));
}

export function lessonBySlug(slug: string): Lesson | undefined {
  return LESSONS.find((l) => l.slug === slug);
}

// The lessons this person has finished, from their learn.done setting,
// keeping only slugs that still name a lesson.
export function doneSet(done: unknown): Set<string> {
  const known = new Set(LESSONS.map((l) => l.slug));
  return new Set(Array.isArray(done) ? done.filter((d): d is string => typeof d === "string" && known.has(d)) : []);
}

// withDone is the learn.done list after marking (or unmarking) one lesson, in
// lesson order, so the stored list reads the same however it was reached.
export function withDone(done: Set<string>, slug: string, on: boolean): string[] {
  const next = new Set(done);
  if (on) next.add(slug);
  else next.delete(slug);
  return LESSONS.map((l) => l.slug).filter((s) => next.has(s));
}

// LAB_DOCS is where a learner with no simulator is sent: the lab brings up a
// simulated fleet in Docker.
export const LAB_DOCS = "https://docs.omniglass.hyperscaleav.com/developer/lab/";
