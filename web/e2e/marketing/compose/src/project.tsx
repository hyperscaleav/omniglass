// The marketing captures' camera (#2299). One scene: the recorded loop, drawn
// at the viewport's size, and the camera moves render.mjs computed from the
// recorder's focus log (cameraTrack in web/e2e/marketinglib.mjs). A move eases
// the video's scale and position together, so the frame glides onto a box.
import { all, createRef, easeInOutCubic, makeProject, useScene, Vector2, waitFor } from '@revideo/core';
import { makeScene2D, Video } from '@revideo/2d';

type Move = { at: number; dur: number; scale: number; x: number; y: number };

const shot = makeScene2D('shot', function* (view) {
  const vars = useScene().variables;
  const src = vars.get('src', '')();
  const width = vars.get('width', 1440)();
  const height = vars.get('height', 900)();
  const duration = vars.get('duration', 0)();
  const moves = vars.get<Move[]>('moves', [])();

  // A point (x, y) of the capture, in viewport pixels, at the frame's centre:
  // the video shifts by the point's offset from centre, times the scale.
  const at = (m: Move) => new Vector2(-(m.x - width / 2) * m.scale, -(m.y - height / 2) * m.scale);

  const video = createRef<Video>();
  view.add(<Video ref={video} src={src} width={width} height={height} play />);

  let t = 0;
  for (const m of moves) {
    yield* waitFor(Math.max(0, m.at - t));
    yield* all(
      video().scale(m.scale, m.dur, easeInOutCubic),
      video().position(at(m), m.dur, easeInOutCubic),
    );
    t = m.at + m.dur;
  }
  yield* waitFor(Math.max(0, duration - t));
});

export default makeProject({ scenes: [shot] });
