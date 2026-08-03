// files.js — turning a checkpoint manifest into something browsable.
//
// The manifest is a flat list of paths. A flat list is the honest shape of the
// data but the wrong shape for reading it: nesting is what tells you that
// `src/lib/api.js` and `src/routes/+page.svelte` are related.

import File from '@lucide/svelte/icons/file';
import FileCode from '@lucide/svelte/icons/file-code';
import FileJson from '@lucide/svelte/icons/file-json';
import FileText from '@lucide/svelte/icons/file-text';
import FileImage from '@lucide/svelte/icons/file-image';
import FileArchive from '@lucide/svelte/icons/file-archive';
import FileCog from '@lucide/svelte/icons/file-cog';
import FileLock from '@lucide/svelte/icons/file-lock';
import Terminal from '@lucide/svelte/icons/terminal';

const BY_EXTENSION = {
  js: FileCode, mjs: FileCode, cjs: FileCode, ts: FileCode, tsx: FileCode, jsx: FileCode,
  svelte: FileCode, vue: FileCode, go: FileCode, rs: FileCode, py: FileCode, rb: FileCode,
  java: FileCode, c: FileCode, h: FileCode, cpp: FileCode, hpp: FileCode, cs: FileCode,
  php: FileCode, swift: FileCode, kt: FileCode, sql: FileCode, html: FileCode, css: FileCode,
  scss: FileCode, wasm: FileCode,
  json: FileJson, jsonl: FileJson, ndjson: FileJson,
  md: FileText, markdown: FileText, txt: FileText, rst: FileText, log: FileText, csv: FileText,
  png: FileImage, jpg: FileImage, jpeg: FileImage, gif: FileImage, webp: FileImage,
  svg: FileImage, bmp: FileImage, ico: FileImage, avif: FileImage,
  zip: FileArchive, tar: FileArchive, gz: FileArchive, tgz: FileArchive, bz2: FileArchive,
  xz: FileArchive, zst: FileArchive, '7z': FileArchive, rar: FileArchive,
  yml: FileCog, yaml: FileCog, toml: FileCog, ini: FileCog, conf: FileCog, cfg: FileCog,
  env: FileLock, pem: FileLock, key: FileLock, crt: FileLock,
  sh: Terminal, bash: Terminal, zsh: Terminal, fish: Terminal, ps1: Terminal
};

const BY_NAME = {
  dockerfile: FileCog, makefile: FileCog, 'go.mod': FileCog, 'go.sum': FileLock,
  'package.json': FileJson, 'package-lock.json': FileLock, license: FileText, readme: FileText
};

export const IMAGE_EXTENSIONS = new Set([
  'png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'ico', 'avif'
]);

export function extensionOf(path) {
  const name = path.split('/').pop() ?? '';
  const dot = name.lastIndexOf('.');
  return dot > 0 ? name.slice(dot + 1).toLowerCase() : '';
}

export function isImage(path) {
  return IMAGE_EXTENSIONS.has(extensionOf(path));
}

/** @returns the icon component for a path. */
export function iconFor(path) {
  const name = (path.split('/').pop() ?? '').toLowerCase();
  return BY_NAME[name] ?? BY_EXTENSION[extensionOf(path)] ?? File;
}

/**
 * Builds a nested tree from the manifest's flat entries. Directories are
 * inferred from path segments, so a manifest that lists only files still
 * produces the folders they live in.
 *
 * @param {Array<{ path: string, size?: number, is_dir?: boolean }>} entries
 */
export function buildTree(entries) {
  const root = { name: '', path: '', type: 'dir', children: new Map() };

  for (const entry of entries ?? []) {
    const parts = entry.path.split('/').filter(Boolean);
    if (parts.length === 0) continue;
    let node = root;

    parts.forEach((part, i) => {
      const last = i === parts.length - 1;
      const path = parts.slice(0, i + 1).join('/');
      if (last && !entry.is_dir) {
        node.children.set(part, { name: part, path, type: 'file', size: entry.size ?? 0 });
        return;
      }
      let child = node.children.get(part);
      if (!child || child.type !== 'dir') {
        child = { name: part, path, type: 'dir', children: new Map() };
        node.children.set(part, child);
      }
      node = child;
    });
  }

  // Directories first, then files, each alphabetical — the order a file
  // browser trains you to expect.
  const sort = (node) => {
    const kids = [...node.children.values()];
    for (const kid of kids) if (kid.type === 'dir') sort(kid);
    kids.sort((a, b) =>
      a.type === b.type ? a.name.localeCompare(b.name) : a.type === 'dir' ? -1 : 1
    );
    node.sorted = kids;
    return node;
  };

  return sort(root).sorted;
}

/** Total bytes across the manifest's files, for the pane's summary line. */
export function totalSize(entries) {
  return (entries ?? []).reduce((sum, f) => sum + (f.is_dir ? 0 : (f.size ?? 0)), 0);
}
