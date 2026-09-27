import { test } from "node:test";
import assert from "node:assert/strict";
import { wordCount, fuzzyScore, buildTree, notePath } from "../src/text.ts";

test("wordCount counts CJK characters and Latin words, not marks", () => {
  assert.equal(wordCount(""), 0);
  assert.equal(wordCount("# 测试笔记"), 4);
  assert.equal(wordCount("hello, world"), 2);
  assert.equal(wordCount("**Jusnote** 的笔记"), 4);
  assert.equal(wordCount("don't stop-me 2026"), 3);
  assert.equal(wordCount("ひらがな カタカナ"), 8);
  assert.equal(wordCount("- [ ] 任务 one"), 3);
});

test("fuzzyScore matches in order and prefers file names and runs", () => {
  assert.equal(fuzzyScore("xyz", "notes/ideas.md"), -1);
  assert.ok(fuzzyScore("ideas", "notes/ideas.md") > 0);
  assert.ok(fuzzyScore("ideas", "notes/ideas.md") > fuzzyScore("ideas", "i/d/e/a/s.md"));
  assert.ok(fuzzyScore("log", "logs/2026-01.md") > fuzzyScore("log", "notes/blog.md"));
  assert.equal(fuzzyScore("", "a.md"), 0);
  assert.ok(fuzzyScore("测试", "笔记/测试.md") > 0);
});

test("buildTree nests folders first, numeric order", () => {
  const t = buildTree(["b.md", "a/x.md", "a/2.md", "a/10.md", "c/d/e.md"]);
  assert.deepEqual(t.map((n) => n.name), ["a", "c", "b.md"]);
  assert.deepEqual(t[0].children.map((n) => n.name), ["10.md", "2.md", "x.md"].sort((a, b) => a.localeCompare(b, undefined, { numeric: true })));
  assert.equal(t[1].children[0].path, "c/d");
  assert.equal(t[1].children[0].children[0].path, "c/d/e.md");
});

test("notePath normalises what was typed", () => {
  assert.equal(notePath(" ideas "), "ideas.md");
  assert.equal(notePath("a\\b"), "a/b.md");
  assert.equal(notePath("/x//y.markdown"), "x/y.markdown");
  assert.equal(notePath(""), "");
});

import { toggleTask, openWikiLink } from "../src/text.ts";

test("toggleTask flips the n-th task, skipping code", () => {
  const text = "- [ ] a\n```\n- [ ] code\n```\n* [x] b\n> 1. [ ] quoted\nplain [ ] no";
  assert.equal(toggleTask(text, 0), text.replace("- [ ] a", "- [x] a"));
  assert.equal(toggleTask(text, 1), text.replace("* [x] b", "* [ ] b"));
  assert.equal(toggleTask(text, 2), text.replace("> 1. [ ] quoted", "> 1. [x] quoted"));
  assert.equal(toggleTask(text, 3), null);
});

test("openWikiLink finds the link being typed", () => {
  assert.equal(openWikiLink("see [[ide"), "ide");
  assert.equal(openWikiLink("see [["), "");
  assert.equal(openWikiLink("see [[done]] and"), null);
  assert.equal(openWikiLink("[[a|lab"), null);
  assert.equal(openWikiLink("no link"), null);
});

import { changedSpan } from "../src/text.ts";

test("changedSpan finds the differing middle", () => {
  assert.deepEqual(changedSpan("测试夹具", "测试用的夹具"), [2, 2, 4]);
  assert.deepEqual(changedSpan("abc", "abc"), [3, 3, 3]);
  assert.deepEqual(changedSpan("a-b", "aXYb"), [1, 2, 3]);
  assert.deepEqual(changedSpan("", "new"), [0, 0, 3]);
});
