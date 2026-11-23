import { describe, expect, it, afterEach } from "vitest";
import { render, cleanup } from "@solidjs/testing-library";
import { CursorProvider, createCursor, useCursor, type Cursor } from "./cursor";

// The shared cursor (#2424): one instant, or none, that every chart under a
// provider reads and any of them sets.
afterEach(cleanup);

describe("cursor", () => {
  it("holds an instant until cleared", () => {
    const c = createCursor();
    expect(c.at()).toBeUndefined();
    c.set(1000);
    expect(c.at()).toBe(1000);
    c.set(undefined);
    expect(c.at()).toBeUndefined();
  });

  it("two readers under one provider share it; a reader outside any provider has its own", () => {
    let a!: Cursor, b!: Cursor, alone!: Cursor;
    const Grab = (p: { to: (c: Cursor) => void }) => {
      p.to(useCursor());
      return null;
    };
    render(() => (
      <>
        <CursorProvider>
          <Grab to={(c) => (a = c)} />
          <Grab to={(c) => (b = c)} />
        </CursorProvider>
        <Grab to={(c) => (alone = c)} />
      </>
    ));
    a.set(42);
    expect(b.at()).toBe(42);
    expect(alone.at()).toBeUndefined();
  });
});
