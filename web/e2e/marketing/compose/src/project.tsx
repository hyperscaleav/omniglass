// The marketing captures' camera (#2299). One scene: the recorded loop, drawn
// at the viewport's size, and the camera moves render.mjs computed from the
// recorder's focus log (cameraTrack in web/e2e/marketinglib.mjs). A move eases
// the video's scale and position together, so the frame glides onto a box.
// While the camera is on a box, the rest of the capture dims (the move's
// spot), so the eye lands on what changed. A wait the recorder played faster
// (a lapse) shows a badge that says so for as long as it runs.
import { all, createRef, easeInOutCubic, makeProject, useScene, Vector2, waitFor } from '@revideo/core';
import type { ThreadGenerator } from '@revideo/core';
import { makeScene2D, Node, Rect, Txt, Video } from '@revideo/2d';

type Box = { x: number; y: number; width: number; height: number };
type Move = { at: number; dur: number; scale: number; x: number; y: number; spot: Box | null };
type Badge = { from: number; to: number; speed: number };

const shot = makeScene2D('shot', function* (view) {
  const vars = useScene().variables;
  const src = vars.get('src', '')();
  const width = vars.get('width', 1440)();
  const height = vars.get('height', 900)();
  const duration = vars.get('duration', 0)();
  const moves = vars.get<Move[]>('moves', [])();
  const badges = vars.get<Badge[]>('badges', [])();
  const dim = vars.get('dim', 0.5)();

  // A point (x, y) of the capture, in viewport pixels, at the frame's centre:
  // the stage shifts by the point's offset from centre, times the scale.
  const at = (m: Move) => new Vector2(-(m.x - width / 2) * m.scale, -(m.y - height / 2) * m.scale);
  // A box of the capture, in the stage's own coordinates (its centre is 0, 0).
  const centre = (b: Box) => new Vector2(b.x + b.width / 2 - width / 2, b.y + b.height / 2 - height / 2);

  const stage = createRef<Node>();
  const shade = createRef<Node>();
  const hole = createRef<Rect>();
  const badge = createRef<Rect>();
  const label = createRef<Txt>();
  view.add(
    <Node ref={stage}>
      <Video src={src} width={width} height={height} play />
      {/* The shade is drawn on its own (cached), so the hole cut from it
          shows the video through, not the backdrop. */}
      <Node ref={shade} cache opacity={0}>
        <Rect width={width} height={height} fill="#000" />
        <Rect ref={hole} width={width} height={height} radius={10} fill="#000" compositeOperation="destination-out" />
      </Node>
    </Node>,
  );
  view.add(
    <Rect ref={badge} x={width / 2 - 150} y={-height / 2 + 52} width={220} height={52} radius={26} fill="rgba(8,12,22,.82)" stroke="#21cab9" lineWidth={2} opacity={0}>
      <Txt ref={label} fill="#e6f7f5" fontSize={24} fontFamily="Inter, system-ui, sans-serif" fontWeight={600} />
    </Rect>,
  );

  function* camera(): ThreadGenerator {
    let t = 0;
    for (const m of moves) {
      yield* waitFor(Math.max(0, m.at - t));
      const lit = m.spot ? [hole().position(centre(m.spot), m.dur, easeInOutCubic), hole().size([m.spot.width, m.spot.height], m.dur, easeInOutCubic)] : [];
      yield* all(
        stage().scale(m.scale, m.dur, easeInOutCubic),
        stage().position(at(m), m.dur, easeInOutCubic),
        shade().opacity(m.spot ? dim : 0, m.dur, easeInOutCubic),
        ...lit,
      );
      t = m.at + m.dur;
    }
    yield* waitFor(Math.max(0, duration - t));
  }

  function* lapses(): ThreadGenerator {
    let t = 0;
    for (const b of badges) {
      yield* waitFor(Math.max(0, b.from - t));
      label().text(`${b.speed}× speed`);
      yield* badge().opacity(1, 0.2);
      yield* waitFor(Math.max(0, b.to - b.from - 0.4));
      yield* badge().opacity(0, 0.2);
      t = b.to;
    }
  }

  yield* all(camera(), lapses());
});

export default makeProject({ scenes: [shot] });
