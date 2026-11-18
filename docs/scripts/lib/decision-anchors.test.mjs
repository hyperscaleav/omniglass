import assert from 'node:assert/strict';
import test from 'node:test';
import { brokenFragments, headingIds, logFragments } from './decision-anchors.mjs';

const log = '<h3 id="adr-0001-a-title">A</h3><h3 id="adr-0002-b-title.2">B</h3>';

test('headingIds collects every id on the page', () => {
  assert.deepEqual([...headingIds(log)], ['adr-0001-a-title', 'adr-0002-b-title.2']);
});

test('logFragments reads absolute links into the log from any page', () => {
  const page = '<a href="/architecture/decisions/#adr-0001-a-title">x</a><a href="/architecture/other/#nope">y</a><a href="#local">z</a>';
  assert.deepEqual(logFragments(page, false), ['adr-0001-a-title']);
});

test('logFragments reads in-page fragments only on the log', () => {
  const page = '<a href="#adr-0001-a-title">x</a><a href="#">top</a>';
  assert.deepEqual(logFragments(page, true), ['adr-0001-a-title']);
  assert.deepEqual(logFragments(page, false), []);
});

test('logFragments ignores a link into the log with no fragment and a longer path', () => {
  const page = '<a href="/architecture/decisions/">x</a><a href="/architecture/decisions/other/#a">y</a>';
  assert.deepEqual(logFragments(page, false), []);
});

test('brokenFragments names the fragments that are not an id', () => {
  const ids = headingIds(log);
  const page = '<a href="/architecture/decisions/#adr-0001-a-title">ok</a><a href="/architecture/decisions/#adr-0001-old-title">bad</a><a href="/architecture/decisions/#adr-0002-b-title.2">ok</a>';
  assert.deepEqual(brokenFragments(page, false, ids), ['adr-0001-old-title']);
});
