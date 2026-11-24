import type { Component } from "solid-js";
import TopBar from "../components/TopBar";
import { pageHeader } from "./handover";

// withTopBar mounts the real top bar over a page, fed from the page header the
// way App feeds it (#2575), so a test sees the trail and the h1 where the
// person does.
export function withTopBar(Page: Component): Component {
  return () => (
    <>
      <TopBar section="" title={pageHeader()?.title} hint={pageHeader()?.hint} trail={pageHeader()?.trail} kind={pageHeader()?.kind} system={pageHeader()?.system} onOpenPalette={() => {}} />
      <Page />
    </>
  );
}
