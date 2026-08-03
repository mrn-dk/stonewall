// Tests for the transcript's Markdown parser. Run with `npm test`.
//
// Uses node:test, so this costs no dependency. The parser is the one piece of
// the dashboard with real logic in it — everything else is rendering — and it
// runs over text an agent produced, which is exactly the input you do not get
// to assume is well behaved.

import test from 'node:test';
import assert from 'node:assert/strict';
import { parseMarkdown, parseInline, safeHref, looksLikeMarkdown } from './markdown.js';

const text = (value) => ({ type: 'text', value });

test('safeHref allows http, https, mailto and relative links', () => {
  assert.equal(safeHref('https://example.dev/a'), 'https://example.dev/a');
  assert.equal(safeHref('http://example.dev'), 'http://example.dev');
  assert.equal(safeHref('mailto:someone@example.dev'), 'mailto:someone@example.dev');
  assert.equal(safeHref('./notes.md'), './notes.md');
  assert.equal(safeHref('/absolute/path'), '/absolute/path');
});

test('safeHref rejects script-bearing schemes', () => {
  assert.equal(safeHref('javascript:alert(1)'), null);
  assert.equal(safeHref('JavaScript:alert(1)'), null);
  assert.equal(safeHref('data:text/html;base64,PHNjcmlwdD4='), null);
  assert.equal(safeHref('vbscript:msgbox'), null);
  assert.equal(safeHref('  javascript:alert(1)  '), null);
});

test('markup in the source is inert text, never markup', () => {
  // The whole point of parsing to a token tree: there is no token type that
  // can carry raw HTML, so an agent cannot emit any.
  const blocks = parseMarkdown('<img src=x onerror=alert(1)>');
  assert.deepEqual(blocks, [
    { type: 'paragraph', children: [text('<img src=x onerror=alert(1)>')] }
  ]);
  const tokens = parseInline('<script>alert(1)</script>');
  assert.ok(tokens.every((t) => t.type === 'text'));
});

test('a link with an unsafe scheme degrades to its label', () => {
  assert.deepEqual(parseInline('[click](javascript:alert(1))'), [text('click')]);
});

test('link destinations may contain balanced parentheses', () => {
  assert.deepEqual(parseInline('[a](https://example.dev/p(1))'), [
    { type: 'link', href: 'https://example.dev/p(1)', children: [text('a')] }
  ]);
});

test('a bare URL does not recurse forever', () => {
  // Regression: the url rule re-parsed its own match as a label, which matched
  // the url rule again. A message containing a plain link hung the tab.
  assert.deepEqual(parseInline('see https://example.dev now'), [
    text('see '),
    { type: 'link', href: 'https://example.dev', children: [text('https://example.dev')] },
    text(' now')
  ]);
});

test('inline emphasis, code and strikethrough', () => {
  assert.deepEqual(parseInline('a **b** c'), [
    text('a '),
    { type: 'strong', children: [text('b')] },
    text(' c')
  ]);
  assert.deepEqual(parseInline('_it_'), [{ type: 'em', children: [text('it')] }]);
  assert.deepEqual(parseInline('~~no~~'), [{ type: 'del', children: [text('no')] }]);
});

test('a code span suppresses emphasis inside it', () => {
  assert.deepEqual(parseInline('`a *b* c`'), [{ type: 'code', value: 'a *b* c' }]);
});

test('headings, rules and blockquotes', () => {
  assert.deepEqual(parseMarkdown('## Title'), [
    { type: 'heading', depth: 2, children: [text('Title')] }
  ]);
  assert.deepEqual(parseMarkdown('---'), [{ type: 'hr' }]);
  assert.deepEqual(parseMarkdown('> quoted'), [
    { type: 'blockquote', children: [{ type: 'paragraph', children: [text('quoted')] }] }
  ]);
});

test('fenced code keeps its language and body verbatim', () => {
  assert.deepEqual(parseMarkdown('```go\nfmt.Println("hi")\n```'), [
    { type: 'code', lang: 'go', value: 'fmt.Println("hi")' }
  ]);
  // Unterminated fences must still terminate the parse.
  assert.deepEqual(parseMarkdown('```\nunclosed'), [
    { type: 'code', lang: '', value: 'unclosed' }
  ]);
});

test('ordered and unordered lists', () => {
  assert.deepEqual(parseMarkdown('- one\n- two'), [
    { type: 'list', ordered: false, items: [[text('one')], [text('two')]] }
  ]);
  assert.deepEqual(parseMarkdown('1. a\n2. b'), [
    { type: 'list', ordered: true, items: [[text('a')], [text('b')]] }
  ]);
});

test('empty and non-string input do not throw', () => {
  assert.deepEqual(parseMarkdown(''), []);
  assert.deepEqual(parseMarkdown(null), []);
  assert.deepEqual(parseMarkdown(undefined), []);
  assert.deepEqual(parseInline(''), []);
});

test('looksLikeMarkdown distinguishes structure from prose', () => {
  assert.equal(looksLikeMarkdown('# heading'), true);
  assert.equal(looksLikeMarkdown('some **bold**'), true);
  assert.equal(looksLikeMarkdown('- a list'), true);
  assert.equal(looksLikeMarkdown('just a sentence'), false);
});

test('a pathological input terminates', () => {
  // Unbalanced emphasis markers used to be a plausible way to wedge the
  // tokeniser; the fallback advances one character, so it always terminates.
  const messy = '*'.repeat(200) + '[' .repeat(200) + '`'.repeat(200);
  assert.ok(Array.isArray(parseInline(messy)));
});
