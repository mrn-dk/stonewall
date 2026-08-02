// markdown.js — a small Markdown parser for transcript content.
//
// Why not `marked` + a sanitiser: this renders text an agent produced, and the
// transcript deliberately has no `{@html}` in it. A library that emits an HTML
// string would put one back, and then correctness rests on the sanitiser being
// configured right forever. This produces a token tree instead, which the
// renderer turns into real elements — agent text can only ever become a text
// node, never markup, and there is nothing to sanitise.
//
// The subset is what a chat transcript actually uses: fenced code, headings,
// lists, blockquotes, rules, paragraphs, and inline code/emphasis/links.

/** Schemes a link may use. Anything else renders as plain text, not a link. */
const SAFE_SCHEME = /^(https?:|mailto:)/i;

export function safeHref(href) {
  const trimmed = (href ?? '').trim();
  // Relative links are fine (no scheme); absolute ones must use a safe scheme.
  // This is what keeps `javascript:` and `data:` out of the transcript.
  if (!/^[a-z][a-z0-9+.-]*:/i.test(trimmed)) return trimmed || null;
  return SAFE_SCHEME.test(trimmed) ? trimmed : null;
}

// --- inline ---------------------------------------------------------------

const INLINE = [
  // Code spans win over everything: backticks suppress emphasis inside them.
  { type: 'code', re: /^(`+)([\s\S]*?[^`])\1(?!`)/ },
  { type: 'strong', re: /^\*\*([\s\S]+?)\*\*/ },
  { type: 'strong', re: /^__([\s\S]+?)__/ },
  { type: 'em', re: /^\*([\s\S]+?)\*/ },
  { type: 'em', re: /^_([\s\S]+?)_/ },
  { type: 'del', re: /^~~([\s\S]+?)~~/ },
  // The destination allows one level of balanced parentheses, so
  // `foo(bar)` in a URL is consumed whole instead of leaving a stray `)`
  // behind in the text.
  { type: 'link', re: /^\[([^\]]*)\]\(\s*([^()\s]*(?:\([^()]*\)[^()\s]*)*)\s*(?:"[^"]*")?\)/ },
  { type: 'autolink', re: /^<((?:https?:|mailto:)[^>\s]+)>/ },
  { type: 'url', re: /^(https?:\/\/[^\s<>()[\]]+)/ }
];

/** @returns {Array<object>} inline tokens */
export function parseInline(src) {
  const out = [];
  let text = '';
  let rest = src;

  const flush = () => {
    if (text) out.push({ type: 'text', value: text });
    text = '';
  };

  while (rest) {
    let matched = false;
    for (const rule of INLINE) {
      const m = rule.re.exec(rest);
      if (!m) continue;

      if (rule.type === 'link' || rule.type === 'autolink' || rule.type === 'url') {
        const bare = rule.type !== 'link';
        const href = safeHref(bare ? m[1] : m[2]);
        const label = m[1];
        if (!href) {
          // Unsafe scheme: keep the text, drop the link.
          text += label;
        } else {
          flush();
          // A bare URL is its own label and must NOT be re-parsed: doing so
          // matches the url rule again and recurses until the stack dies.
          // Only a bracketed link has a label worth parsing for emphasis.
          out.push({
            type: 'link',
            href,
            children: bare ? [{ type: 'text', value: label }] : parseInline(label)
          });
        }
      } else if (rule.type === 'code') {
        flush();
        out.push({ type: 'code', value: m[2].trim() });
      } else {
        flush();
        out.push({ type: rule.type, children: parseInline(m[1]) });
      }

      rest = rest.slice(m[0].length);
      matched = true;
      break;
    }

    if (!matched) {
      text += rest[0];
      rest = rest.slice(1);
    }
  }

  flush();
  return out;
}

// --- blocks ---------------------------------------------------------------

const FENCE = /^ {0,3}(`{3,}|~{3,})\s*([\w+-]*)\s*$/;
const HEADING = /^ {0,3}(#{1,6})\s+(.*)$/;
const HR = /^ {0,3}([-*_])(?:\s*\1){2,}\s*$/;
const BLOCKQUOTE = /^ {0,3}> ?(.*)$/;
const UL = /^(\s*)[-*+]\s+(.*)$/;
const OL = /^(\s*)(\d+)[.)]\s+(.*)$/;

/**
 * Parses Markdown into a block token tree.
 * @param {string} src
 */
export function parseMarkdown(src) {
  const lines = String(src ?? '').replace(/\r\n?/g, '\n').split('\n');
  const blocks = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];

    if (!line.trim()) {
      i++;
      continue;
    }

    const fence = FENCE.exec(line);
    if (fence) {
      const marker = fence[1][0];
      const lang = fence[2] || '';
      const body = [];
      i++;
      while (i < lines.length) {
        const close = FENCE.exec(lines[i]);
        if (close && close[1][0] === marker) {
          i++;
          break;
        }
        body.push(lines[i]);
        i++;
      }
      blocks.push({ type: 'code', lang, value: body.join('\n') });
      continue;
    }

    const heading = HEADING.exec(line);
    if (heading) {
      blocks.push({
        type: 'heading',
        depth: heading[1].length,
        children: parseInline(heading[2].trim())
      });
      i++;
      continue;
    }

    if (HR.test(line)) {
      blocks.push({ type: 'hr' });
      i++;
      continue;
    }

    if (BLOCKQUOTE.test(line)) {
      const body = [];
      while (i < lines.length && BLOCKQUOTE.test(lines[i])) {
        body.push(BLOCKQUOTE.exec(lines[i])[1]);
        i++;
      }
      blocks.push({ type: 'blockquote', children: parseMarkdown(body.join('\n')) });
      continue;
    }

    if (UL.test(line) || OL.test(line)) {
      const ordered = OL.test(line);
      const items = [];
      while (i < lines.length) {
        const m = ordered ? OL.exec(lines[i]) : UL.exec(lines[i]);
        if (!m) break;
        const content = [ordered ? m[3] : m[2]];
        i++;
        // Continuation lines: indented, and not the start of the next item.
        while (
          i < lines.length &&
          lines[i].trim() &&
          !UL.test(lines[i]) &&
          !OL.test(lines[i]) &&
          /^\s{2,}/.test(lines[i])
        ) {
          content.push(lines[i].trim());
          i++;
        }
        items.push(parseInline(content.join(' ')));
      }
      blocks.push({ type: 'list', ordered, items });
      continue;
    }

    // Paragraph: consume until a blank line or the start of another block.
    const para = [];
    while (
      i < lines.length &&
      lines[i].trim() &&
      !FENCE.test(lines[i]) &&
      !HEADING.test(lines[i]) &&
      !HR.test(lines[i]) &&
      !BLOCKQUOTE.test(lines[i]) &&
      !UL.test(lines[i]) &&
      !OL.test(lines[i])
    ) {
      para.push(lines[i].trim());
      i++;
    }
    blocks.push({ type: 'paragraph', children: parseInline(para.join('\n')) });
  }

  return blocks;
}

/** True when the text carries enough structure that rendering it helps. */
export function looksLikeMarkdown(text) {
  return /(^|\n)\s*(#{1,6}\s|[-*+]\s|\d+[.)]\s|>\s|```)|\*\*|`[^`]+`|\[[^\]]+\]\(/.test(
    String(text ?? '')
  );
}
