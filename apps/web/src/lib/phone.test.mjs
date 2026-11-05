// Run with `npm test` (Node 22.18+/24 strips the TypeScript types itself).
import { test } from "node:test";
import assert from "node:assert/strict";
import { displayPhone, ltr, normalizePhone } from "./phone.ts";

test("normalizePhone matches the server's NormalizePhone", () => {
  assert.equal(normalizePhone("0501112244", "AE"), "+971501112244");
  assert.equal(normalizePhone("501112244", "AE"), "+971501112244");
  assert.equal(normalizePhone("971501112244", "AE"), "+971501112244");
  assert.equal(normalizePhone("+971 0501112244", "AE"), "+971501112244");
  assert.equal(normalizePhone("00966551230003", "AE"), "+966551230003");
  assert.equal(normalizePhone("0551230003", "SA"), "+966551230003");
  assert.equal(normalizePhone("3312 3456", "QA"), "+97433123456");
  assert.equal(normalizePhone("0551230003", "QA"), null);
  assert.equal(normalizePhone("05011", "AE"), null);
});

test("displayPhone shows the dial code once, whatever was typed", () => {
  assert.equal(displayPhone("971501112244", "AE"), "+971 501112244");
  assert.equal(displayPhone("0501112244", "AE"), "+971 501112244");
  assert.equal(displayPhone("+971501112244", "AE"), "+971 501112244");
  assert.equal(displayPhone("00966551230003", "AE"), "+966 551230003");
  assert.equal(displayPhone("055 123 0003", "SA"), "+966 551230003");
  assert.equal(displayPhone("+447700900123", "AE"), "+447700900123");
});

test("ltr wraps in a left-to-right isolate", () => {
  assert.equal(ltr("+971 501112244"), "\u2066+971 501112244\u2069");
});
